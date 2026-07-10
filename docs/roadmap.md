# 🎼 Regente — Roadmap

> **Fonte única de status (single source of truth) E tracking de tudo que foi entregue.**
> Este arquivo é a verdade sobre "o que já foi feito / o que falta" — e o §✅ Entregue é
> detalhado o suficiente pra cada tópico virar **doc/feature** depois. O README é só
> **apresentação de produto** (conceito, capacidades, guias) e aponta pra cá; não repete
> roadmap. Se algo divergir, ESTE arquivo vence.
>
> **Estrutura:** [§🔜 Backlog](#-backlog-o-que-falta) = o que falta (a lista que a gente faz
> crescer) · [§✅ Entregue](#-entregue-tracking-por-tópico) = tudo pronto, por tópico ·
> [§📜 Changelog de entregas](#-changelog-de-entregas) = o mesmo em ordem cronológica, com o porquê.
>
> ⛔ **REGRA DE STATUS.** A **§🔜 Backlog** é o **ÚNICO registro do que está aberto** — nada
> de pendente pode existir só numa seção de baixo. As seções marcadas *SPEC/histórico* (visão de
> produto, features avançadas) descrevem o *racional*, não o status: se divergirem da §Backlog,
> a §Backlog vence. Ao **entregar** um item: tire a linha do §Backlog, escreva o tópico detalhado
> no §Entregue e some uma linha no §Changelog. Ao **abrir** um item novo: só adicione no §Backlog.
> Sem barras de progresso nem porcentagens — de propósito (confundem mais do que ajudam).
>
> Documento vivo · revisão **2026-07-08**.
> Estratégia de arquitetura em [`arquitetura-futuro.md`](arquitetura-futuro.md);
> apresentação de produto no [`../README.md`](../README.md).

## 📊 Visão geral

> Sem barras de progresso ou porcentagens — de propósito: cada trilha ou está **Entregue**
> (com tudo detalhado em §✅ Entregue, tópico a tópico, pra depois virar doc/feature) ou tem
> itens no **Backlog** (§🔜 Backlog, a lista que a gente vai fazendo crescer). O que não está
> em nenhum dos dois não é escopo ainda.

**Trilhas ENTREGUES** (detalhe em [§✅ Entregue](#-entregue-tracking-por-tópico)):

- **Núcleo / Control-M** — GitOps, daily imutável, dependências com condições, Force Order, executores.
- **Identidade visual / UI** — logo próprio, topbar premium, 13 temas, login, sidebars flutuantes, canvas.
- **Alerting** — motor de regras + routing multi-canal por regra + cooldown + ciclo de vida.
- **Resiliência operacional (R1–R7)** — supervisão, panic-recovery, health real, DR/backup, auto-SLO.
- **Serverless portátil** — gatilho externo, transporte plugável, WASM, NATS, adapters de nuvem.
- **Enterprise readiness** — Postgres/HA, segurança (RBAC/SSO/mTLS/SIEM), operação, qualidade, backlog E1–E6.
- **Escala Control-M (100k–1M/dia)** — write-path (1M/17s), read-path paginado, UI ViewPoint validada @1M.
- **Aprofundamento Control-M** — lifecycle da daily, On-Do, cyclic, CONFIRM, DATABASE, `%%` vars, CTM-1/2/3.
- **Diferenciais além do Control-M** — Explain/Diff/Blast/Dry Run/Neighborhood/RCA/Event log/NL-query + D-1…D-15.
- **Jobs as code (modo CODE) v1** — editor YAML do working set no Design (o aperfeiçoamento CODE-1 é backlog).

**Trilhas com itens em ABERTO** (detalhe em [§🔜 Backlog](#-backlog-o-que-falta)):

- **Refinamento UI** — polimento residual (virtualizar a sidebar legada · override de columns por folder). Drawer do job já redesenhado (8 abas, 2026-07-09).
- **Agent-native (MCP)** — servidor pronto: **11 read + 11 write** gated (MCP-1 fechado 2026-07-08).
- **Jobs as code** — CODE-1 (editor rico, lint live, diff visual, sync com canvas, tabs por job).
- **Features avançadas** — camadas opcionais pós-núcleo (schema por tipo, MFT, CLI/SDK, site de docs…).
- **Validação em infra real** — resíduos (secrets via provider · SSH agente como serviço).
- **Fase Z — divulgação** — case study + post LinkedIn (último gate, quando o backlog estiver onde você quer).

> 🏁 **Marco (2026-06-24):** **todas as trilhas estruturais em 100%**, incluindo **Escala Control-M (100k–1M/dia)
> end-to-end**: write-path materializa **1M em 17s** (P1), read-path serve **summary 51ms / page 18ms @100k**
> (P2), e a **UI por ViewPoint server-driven foi validada AO VIVO com 1.000.000 de jobs** (P3) — dashboard
> instantâneo, folder aberta em ~39ms, lista virtualizada, sem nunca baixar o dia inteiro.

Legenda: ✅ pronto · 🟡 em andamento · ⬜ a fazer · ⭐ recomendado · 🔴 prioridade

---

## 🔜 Backlog (o que falta)

> **Pergunta "o que falta?" → responde AQUI e só aqui.** Esta lista contém **TODO** item
> em aberto do projeto: se não está abaixo, está **entregue** (§✅ Entregue) ou nunca foi
> escopo. Cada item tem um **ID estável** e um **ponteiro `→ §…`** pra spec. É a área que a
> gente vai **fazendo crescer** — quando um item fecha, ele sai daqui e vira um tópico
> detalhado em §✅ Entregue (+ linha no changelog). As caixinhas espalhadas nas seções de
> baixo **não valem** como status (ver ⛔ REGRA DE STATUS no topo).

### Features avançadas — pós-núcleo (→ §Features avançadas)
- [ ] **ADV-4** — MFT (FILE_TRANSFER nativo).
- [ ] **ADV-5** — Archives / Retention (relaciona E2).
- [ ] **ADV-6** — CLI / SDK.
- [ ] **ADV-7** — Site de docs.
- [ ] **ADV-8** — Executores AWS extras (Batch/Glue/Step) — validação em conta paga fora de escopo por decisão.

### Contrato de API / OpenAPI (Swagger)
- [ ] **API-1** — Spec OpenAPI **curada** (não auto-doc dos ~137 handlers) + Swagger UI embutido
  single-origin (mesmo padrão do `-spa-dir`, self-contained, zero CDN). Cobre só a **superfície de
  integração** (~15–20 rotas que um sistema externo realmente chama), NÃO as internas da SPA:
  `POST /ingest` + external events · quick-actions HMAC `/qa/{token}` · instances query + lifecycle
  (`hold`/`release`/`rerun`/`cancel`/`confirm`/`set-ok`) · `/daily/report` · `/forecast` · `/health` ·
  `/metrics`. Vira **contrato enterprise** e, de quebra, **artefato da Fase Z**.
  - **⏳ CONDIÇÃO DE TIMING (não fazer antes):** os consumidores de HOJE não pagam de volta —
    a SPA já é tipada no front e a MCP é agent-native (LLM lê schema MCP, não OpenAPI). Fazer agora =
    imposto de manutenção + risco de spec divergir do código (pior que não ter). **Implementar quando
    o PRIMEIRO destes acontecer:** (a) aparecer um **integrador/consumidor externo real** que precise
    scriptar contra a API (CI/CD, ticketing, ingest de sistema terceiro); OU (b) na **Fase Z**, como
    peça de divulgação/case study. O que vier primeiro dispara; até lá fica parado de propósito.
  - **Gotcha:** OpenAPI 3.0 não modela o verbo custom `QUERY` (tooling engasga). Documentar as formas
    POST gêmeas que já existem (`POST /instances/query`, `POST /query`) e ignorar o `QUERY` na spec.
  - **NÃO fazer via `swaggo` full-annotation** dos handlers — apodrece e a maioria das rotas é interna
    da SPA. Preferir `openapi.yaml` escrito à mão pra superfície pequena. Relaciona ADV-6 (SDK gerável
    da spec) e ADV-7 (site de docs). → §Enterprise readiness, §Agent-native (MCP), §Fase Z

### Validação em infra real — resíduos (→ §Validação em infra real)
- [ ] **VAL-1** — Secrets via provider (env / `-secrets-file`): resolver `github_token`/`webhook_secret`.
- [ ] **VAL-2** — SSH: agente instalado como serviço (systemd / Task Windows) em host com sshd.

### 🏁 Fase Z — ÚLTIMO gate
- [ ] **Z** — Case study técnico + post LinkedIn. **Só quando o backlog acima estiver onde você quer.** Não é o próximo passo.

---

# ✅ Entregue *(tracking por tópico)*

> Tudo que já foi construído e validado, agrupado por trilha e **detalhado o suficiente pra
> virar doc/feature depois**. Quando um item do Backlog fecha, o detalhe vem pra cá. Para a
> ordem cronológica (por data/commit), ver o [§📜 Changelog de entregas](#-changelog-de-entregas)
> no fim. Convenção: `✅` entregue · `◑` parcial (o resto está no Backlog) · `○` opt-in futuro.

## 🟢 Fundação — *pronta*

```
✅ GitOps (Publish · webhook · drift · deep-links · PAT via UI)
✅ Paridade Control-M (calendars · resources · conditions · vars · SLA · forecast)
✅ Daily imutável · dependências com condições · Force Order
✅ Executores: COMMAND · SCRIPT · HTTP · SSH agentless · WASM · targeting por agente
✅ Stream stdout/stderr · retry de execution · /metrics Prometheus
✅ Auth por agente (token dedicado, além do token global) + gestão de tokens na UI
✅ Agente instalável como serviço (systemd no Linux · Tarefa Agendada no Windows)
✅ Webhook secret (HMAC do GitHub) configurável em runtime pela UI
✅ Token do GitHub (PAT) configurável pela UI, persistido server-side (sem env obrigatória)
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
✅ Minimap de navegação — ver "Minimap REVISTO" abaixo (superou este protótipo)
✅ Aba Schedule do job redesenhada (2026-06-29) — `ScheduleEditor`: (1) SAÍRAM "dia útil" e "regra avançada"
   da frequência (dia útil depende de feriados/calendário de cada lugar; o Regente não adivinha) → ficam só
   daily/weekly/monthly; (2) os CALENDÁRIOS entraram na própria aba (fundiu a aba "Calendars" separada) e
   trabalham JUNTO com as regras como include/exclude, cada calendário anexado já mostra a tradução do que ele
   faz (ex.: "seg–sex, menos 3 feriados"); (3) PREVIEW = gist em linguagem natural + **CALENDÁRIO REAL** estilo
   Control-M "View Scheduling" (12 mini-meses, seletor de ano) onde os dias DESTACADOS são EXATAMENTE os que o
   job roda — vindos do backend `POST /api/schedule/preview` → `Scheduler.SchedulePreview` → `IsScheduledOn`
   (a MESMA regra da daily, fonte única; destacado=roda, apagado=não roda). 4 testes Go + endpoint validado
   ao vivo. Aba "Calendars" do drawer removida. (browser-mode sem server: mostra aviso, calendário exato exige server.)
✅ Janela de info do job (drawer do Monitoring) REDESENHADA (ENTREGUE 2026-07-09) — de 2 abas
   (details/log) para 8 abas nomeadas em inglês com botões em **pill de luxo** tematizados
   (`.v2-tabrail`/`.v2-tab` em tokens.css, trilho com scroll + fade nas bordas): **General**
   (job details com Application placeholder · Description · Timeline · **Parameters** — variáveis
   snapshotadas ∪ localVars F18 · Execution config) · **Output** · **Logs** (intacto) ·
   **Statistics** (métricas da run + histórico de duração) · **Schedule** (tradução READ-ONLY do
   schedule de desenho: frequência/dias/meses/roll/janela/cyclic/keep-active/calendars) ·
   **Dependencies** (depends-on/triggers + recursos F15 + conditions IN/OUT F16 + SLA F19 +
   sub-workflow F17) · **Neighborhood** (vizinhança ±raio + blast) · **Why not?** (Explain/RCA).
   Habilitou-se o dado que faltava: o `ServerApiAdapter` DROPAVA `description`/`resources`/
   `conditionsIn·Out`/`variables`(F18)/`environment`/`sla`/`subWorkflow` no `toWeb` — passou a
   carregá-los E round-tripar no `toServer` (fix de perda silenciosa: editar+salvar no Design um
   job importado apagava esses campos). Também corrigido `--v2-bg-deep` (token inexistente →
   `--v2-bg-canvas`). Validado AO VIVO (as 8 abas renderizando com def+instance semeadas; zero
   erro de console). PENDENTE (próximo passo): editor de Parameters/Description/Application no
   Design — hoje Monitoring mostra read-only (a instance é imutável).
✅ Design: LISTA de labels dos jobs da folder (ENTREGUE 2026-07-01) — a aba Folders da `DesignSidebarV2`
   lista os jobs de cada folder aberta como linhas clicáveis (`JobRow`, hover destaca) que navegam/
   centralizam o canvas no nó (`onJobClick`→`focusNode`), pra localização rápida sem caçar nó por nó.
✅ Highlight NEON do(s) job(s) selecionado(s) (ENTREGUE 2026-07-08) — selecionar 1 ou N jobs acende cada card
   com borda + halo neon na cor do tema (`--v2-accent-brand`/`--v2-accent-glow` no `JobNodeV2` quando `selected`),
   em Design E Monitoring. O "azulzão tipo Windows" da multi-seleção (o bounding box `.react-flow__nodesselection-rect`
   do ReactFlow) foi neutralizado (transparente, sem borda) e o rubber-band (Shift+arrasto) tematizado. A seleção
   PERSISTE entre rebuilds do canvas (tick do Monitoring / drawer do Design): `displayNodes` reaplica `selected` a
   partir do `selectedIds` sobre o prop controlado — o highlight não pisca mais nem some. Câmera intocada ao selecionar.
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
✅ Layout de jobs — grade pros SOLTOS, fluxo pros DEPENDENTES (por folder). (Override por folder ✅ UI-3 2026-07-09.)
   ✅ FASE 1 (ENTREGUE 2026-06-25): `layoutFolderInner` particiona conectados (dagre TB, intacto) vs soltos
      (GRADE com wrap: 10 cols, 11º→linha2/colA; alargamento cols=max(10,ceil(N/30)) após 30 linhas). Contrato
      InnerLayout inalterado. Math validada (n=12→11º em linha2/colA; n=600→20cols/30linhas). Defaults
      LAYOUT_COLUMNS=10 / LAYOUT_MAX_ROWS=30 hardcoded. (lib/layout.ts = dead code, mesmo bug, p/ limpar.)
   ✅ FASE 2 (ENTREGUE 2026-06-25): `columns`/`maxRows` configuráveis em Settings (aba Geral › Visualização,
      via localStorage + evento `regente:layout-changed` → re-layouta na hora). Threaded por LayoutConfig em
      layoutFolderInner/composeColumns/build*Canvas.
   ✅ FASE 2b — OVERRIDE POR FOLDER (ENTREGUE 2026-07-09, UI-3): `layout:{columns,maxRows}` no stub
      `.regente-folder.yaml` (viaja com o workspace Git, não é pref de browser); `PUT /api/folders/{name}/layout`
      + GET /api/folders devolve; front resolve `override ?? global` em composeColumns (Monitoring E Design);
      editor no card do FolderManager (ícone ⊞, vazio = herda, badge "⊞ 4×·"); broadcast folder.changed
      re-grida AO VIVO. Ver changelog.
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
✅ Cap de 2000 do Monitoring legado é POUCO + ENGANA (ENTREGUE 2026-07-09, UI-1) — a sidebar ACTIVE JOBS
   virou VIRTUALIZADA DUAL-MODE e mostra o DIA INTEIRO (validado AO VIVO @1.000.000): dia ≤ cap = dados
   locais como sempre (DOM enxuto); dia > cap = windowed server-driven (summary → folders/contagens REAIS;
   `/api/instances/page` ganhou `offset` p/ random-access no salto de scrollbar; placeholders + fetch por
   janela; filtro/busca reescopados NO server; altura >12M px comprimida por fator — 1M×30px estoura o teto
   do browser). Header = "N de <total real do /summary>", nunca truncado disfarçado; aviso "grafo: primeiros
   <cap>" + botão ViewPoint; clique em row fora do espelho puxa a instance (drawer abre); cap do CANVAS
   configurável em Settings (500–5000, teto do parseLimit) com re-fetch ao vivo. Ver changelog.
✅ Aba de AGENTES (em Settings/Config) — visão e CONTROLE da frota.
   ✅ LISTA CONSOLIDADA (ENTREGUE 2026-06-25): aba "Agentes" com a frota online + offline (last-seen) +
      CONTADOR ("N de M online"); cada agente reporta metadata no handshake (os/arch/host/versão/started);
      migration v6 enriquece a tabela `agents`; `GET /api/agents` = online (verdade do hub) + DB. CLIQUE →
      MODAL de detalhe (SO/arch, host, versão, UPTIME, conectado há, 1ª vez visto, último sinal, capabilities).
      Auto-refresh 5s. Gestão de tokens junto. `AgentsManager.tsx`. 2 testes + validado ao vivo (2 agentes
      reais → offline com last-seen ao matar).
   ✅ PING ATIVO (ENTREGUE 2026-06-25): round-trip ping/pong pelo /ws/agent (server manda {event:ping,pingId},
      agente responde {event:pong}); `POST /api/agents/{id}/ping` → {online, ok, latencyMs, error} (timeout 5s);
      `pingRegistry` correlaciona o pong; botão "ping" por agente + "ping todos" na UI, chip com latência/timeout/
      offline. 1 teste (WS real in-process) + validado ao vivo (agente real → ok; inexistente → offline).
   ✅ CROSS-NÓ (multi-node R5) — ENTREGUE 2026-07-03: a frota reflete o CLUSTER inteiro, não só este nó.
      `bus.Distributed.RemoteAgents()` expõe a presença R5 (agents vistos em outros nós, com TTL); `GET
      /api/agents` faz a UNIÃO hub-local ∪ presença-remota → um agent conectado no node-2 aparece ONLINE
      (campo `node` = nó dono) em vez de fantasma offline, e `local` diz se é pingável daqui (só o do próprio
      nó). Agent remoto sem linha no DB deste nó ainda entra na frota. Config `Presence`/`NodeID` (nil =
      single-node, comportamento idêntico ao anterior). UI: chip "⇄ node" no agent remoto + linha "Nó" no
      detalhe; "ping" só nos locais. 1 teste (`TestAgents_CrossNodePresence`, presença mockada sem NATS).

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
        ✅ Sidebar ACTIVE JOBS legada VIRTUALIZADA (UI-1, 2026-07-09): dual-mode windowed sobre summary/page
        (+`offset` p/ random-access), re-validada AO VIVO @1M — 37 rows no DOM, total real no header, cap do
        canvas configurável. Ver changelog.
```

## 🎛️ Aprofundamento Control-M *(trilha fechada — 2026-07-06)*

```
✅ Ciclo de vida da daily (carry-over / Keep Active) — RUNNING/HELD atravessam a virada;
   NOTOK não-tratado persiste +1 diária (ou N via schedule.keepActive); order_date avança
   mantendo id/status/histórico; idempotente. Migration v5.
✅ Actions / On-Do (Control-M On/Do) — motor nas 3 dimensões (result/attempt/runtime) + 4 ações
   (notify/set-condition/run-job/set-ok), idempotência por ledger (migration v7). UI: aba On/Do no drawer.
✅ Daily server-side configurável — settings.daily_at (HH:MM, sem restart) + GET /api/daily/status.
✅ WAIT EVENT — sucessor de job falho/rodando fica WAITING (não é mais auto-cancelado); rerun/Set OK destravam.
✅ WAIT AGENT — sem agente, o job nem é reivindicado (zero churn/flicker); card azul claro; conectou → dispatch.
✅ Variáveis %% (AutoEdit) — %%ODATE/RUNDATE/JOBNAME… interpoladas nos params; def.variables + globais F18.
✅ FILE_WATCH — executor no agente (poll do arquivo + tamanho estável + timeout); capability própria.
✅ Calendários + shift (roll) — schedule.shift next/prev-businessday na fonte única IsScheduledOn.
✅ Condition vazia = on-success (fix de segurança semântico no edgeState).
✅ Cyclic runtime — job OK re-arma a instance +intervalMin (cycleRuns/cyclicMaxRuns/windowTo); NOTOK não cicla.
✅ CONFIRM (Wait for confirmation) — gate WAIT_CONFIRM; nem o Force bypassa; rerun re-exige.
✅ Job DATABASE — SQL em Postgres/MySQL/SQLite pelo agente, drivers pure-Go (sem CGO/JDBC).
✅ SET de var em runtime — %%SET NOME=VALOR no output → VariableStore global; cálculo de datas %%ODATE±N/±NB.
✅ WINDOW_CLOSED — WAITING depois de windowTo (não submete mais no dia).
✅ ViewPoints salvos (filtros nomeados do Monitoring) + dashboards de presets do /summary.
✅ CTM-1 %%SETLOCAL — var com escopo LOCAL por instance (local_vars JSON, migration v10, aplicada antes do retry;
   precedência Runtime > Local > Definition > Global; sobrevive a rerun/ciclo, nunca vaza).
✅ CTM-2 tokens nativos de data — %%EOM/BOM/EOY/BOY/NEXTBD/PREVBD/FIRSTBD/LASTBD do ODATE, cientes do calendar
   do job e componíveis com offset (%%EOM-1B).
✅ CTM-3 Mass Update / Find & Update RICO — critério (ids/folder/jobType/regex/campo-vazio) → operação
   (set-field/find-replace/add-remove de action/upstream/variable/condition) → preview com diff por job →
   apply transacional por item → undo por session (pilha cap 10). Schedule.Description adicionado ao Go.
```

## 🤖 Agent-native (MCP)

```
✅ Servidor MCP (server/cmd/mcp, stdio JSON-RPC, pure-Go) — fachada sobre a REST; validado por
   pipe JSON-RPC como o Claude Desktop faz.
✅ Tools READ (11): daily_summary · forecast · list_instances · explain_job · blast_radius ·
   job_neighborhood · root_cause · diff_daily · dry_run · event_log · query (NL-query).
✅ Tools WRITE (11, gated por -allow-writes + aprovação do cliente, todas destructiveHint) — MCP-1
   FECHADO (2026-07-08): hold · release · cancel · confirm · rerun · set_ok · force_order ·
   pause_folder · resume_folder · bulk_action · ingest_event. Cada uma é fachada de 1 endpoint REST
   já existente (writeID p/ as unitárias; strSlice p/ ids[]/conditions[]; gate central). Doc: docs/mcp.md.
```

## 🌟 Diferenciais além do Control-M *(trilha fechada — leva 1: 2026-07-03 · leva 2 D-1..D-15: 2026-07-08)*

> Onde o Regente **passa** o Control-M. Todos read-only-aditivos ou opt-in — nenhum toca o
> tick/dispatch/gating sem passar pela fonte única do gateInstance.

**Leva 1 — observabilidade (substratos do MCP):**
```
✅ Explain "por que não rodou?" — motor determinístico (sem IA): WAIT_WINDOW/DEP/CONDITION/RESOURCE/AGENT/
   CONFIRM/WINDOW_CLOSED, construído como FONTE ÚNICA do gating (o mesmo avaliador do tick). GET /explain.
✅ Diff de Daily — compara 2 order_date via snapshots congelados (commit_sha + definition_snapshot); diff
   por-campo; fast-path same-commit. GET /api/daily/diff.
✅ Blast Radius — "se eu cancelar/segurar X agora?": BFS reverso de deps (downstream/SLA/folders/cascata),
   só o raio (barato a 1M). GET /api/instances/{id}/blast-radius.
✅ Dry Run — simula a daily de qualquer data sem materializar (RUN/WAIT/BLOCKED/NOT_SCHEDULED + razão);
   reusa IsScheduledOn. GET /api/daily/dryrun.
✅ Job Neighborhood — grafo local (ancestrais + descendentes até N saltos, status por instance).
✅ RCA — sobe a cadeia de upstreams falhos e aponta a raiz que caiu por conta própria.
✅ Event log CQRS-lite — read-model cross-instance sobre instance_events (feed/auditoria do dia, cursor keyset).
✅ NL-query — parser de intenção determinístico PT/EN (summary/list/count/explain) → consulta estruturada.
```

**Leva 2 — orquestração híbrida, DevEx, enterprise, "wow" (D-1..D-15):**
```
✅ D-1  Human-in-the-loop + long-running — retryDelayMin agenda o retry via scheduled_at DURÁVEL (sobrevive a
        restart/virada da daily; carryDecision trata WAITING-com-attempts>1 como NOTOK-em-tratamento). Compõe
        com Confirm/keepActive p/ "aprovação manual + retry após 3 dias". (scheduler.maybeRetry/carryOver)
✅ D-2  Pause/resume de WORKFLOW com estado preservado — POST /api/folders/{name}/pause|resume: WAITING↔HELD
        em massa, SET-BASED (1 UPDATE + INSERT…SELECT de eventos numa tx); attempts/cycle_runs/scheduled_at/
        confirmed intactos; RUNNING não é pausável; carry-over persiste HELD entre diárias. (api/workflow.go)
✅ D-3  Event-driven confiável — POST /api/events/ingest: evento externo IDEMPOTENTE (dedupe pela PK id do
        emissor, migration v13) seta conditions e/ou force-ordena e cutuca o tick (sem polling); payload +
        applied persistidos p/ forense. (api/ingest.go, external_events)
✅ D-4  Performance forecasting — GET /api/analytics/forecast: p50/p90 + tendência (regressão linear) +
        previsão + ETA do RUNNING, por histórico OK; /api/analytics/durations dá p50/def p/ a Timeline.
        Front: ForecastPanel (sparkline SVG puro no drawer). (scheduler/perfforecast.go)
✅ D-5  Query estruturada composta e tipada — POST /api/instances/query (ranges de data, IN de folders/
        statuses, flags forced/carried/late, groupBy, keyset cursor; parse estrito → 400) + o método HTTP
        QUERY (draft IETF, safe+idempotente com body) na MESMA handler. (api/instquery.go)
✅ D-6  Schedule as Code — modo CODE no Design (v1) + DSL Go pkg/jobdsl (builder fluente → YAML byte-compatível
        com o workspace, Validate(), output determinístico, golden/example test).
✅ D-7  Testing framework — regente test <job.yaml|ws>: parse estrito + validação + grafo (deps órfãs/ciclos)
        + policy + SIMULAÇÃO com o engine REAL (scheduler.DryRun); exit 1 no CI, -json. (server/cmd/regente)
✅ D-8  Local dev mode — regente dev daily: server local descartável (SQLite temp, demo-mode, daily no boot).
✅ D-9  Multi-environment promotion Git-native — regente promote -from -to: snapshot da origem SUBSTITUI o
        destino (definitions+calendars+policies.yaml; add/update/delete, não merge), diff/-dry-run, commit+push.
✅ D-10 Policy as Code — pkg/policy lê policies.yaml da raiz do workspace (requireSLA/Retries/Description/
        Calendar/idPattern/allowedJobTypes/maxRetries/forbidDryRun; error|warn|off; exemptFolders); gate do
        publish (422) e do regente test; feedback no modo CODE; GET /api/policy. Política quebrada BLOQUEIA.
✅ D-11 Chaos engineering — POST /api/instances/{id}/inject-failure: falha sintética pelo FinishInstance REAL
        (retry/On-Do/alerting reagem como numa falha orgânica); botão 💥 Chaos no drawer. (scheduler/chaos.go)
✅ D-12 Visual Schedule Editor — Timeline (Gantt) da daily: barras reais (started→finished/agora) e previstas
        (p50 histórico, hachura), régua 00–24h, linha do agora; overlay no Monitoring (cap 400 linhas).
✅ D-13 Templates reutilizáveis — job_templates (CRUD, migration v13) + "☆ Template" no drawer (salva a FORMA,
        descarta id/team/upstream) + aba Templates na palette (instancia job novo). (api/wow.go)
✅ D-14 Self-Service Portal — def.selfService + rota /portal (mobile-friendly): só jobs expostos, botão Rodar,
        gate próprio (qualquer logado; opt-in versionado no YAML; job não-exposto = 404, não vaza catálogo).
✅ D-15 Mobile-friendly alerts — quick-actions assinadas (HMAC-SHA256, escopo instance+action+exp, TTL 24h,
        allowlist sem cancel) anexadas aos alertas Slack/webhook; rota pública GET/POST /qa/{token} (GET
        confirma, POST executa). (internal/quickaction)
```

## 💻 Jobs as code (modo CODE no Design) — trilha fechada (CODE-1 em 2026-07-09)

```
✅ Botão CODE no Design → o palco vira editor YAML do working set da session.
✅ GET/POST /api/design/sessions/{sid}/code — multi-doc no dialeto do workspace, parse ESTRITO (KnownFields),
   plano creates/updates/deletes/unchanged, dry-run (Validar), delete-por-ausência gated (allowDelete).
✅ CodeModeView — highlight YAML por regex, zero dependência nova; escopo com chave estável.
✅ CODE-1 (ENTREGUE 2026-07-09, escopo decidido pelo usuário — "mata o CODE-1, tira o Matrix, linha luxo"):
   • RESKIN LUXO — a estética Matrix (digital rain, verde fósforo #00ff41) foi aposentada; o modo código
     entrou na linha luxo do Regente (título serif itálico, accent do TEMA em tudo, zero cor hardcoded de
     chrome; botão CODE da topbar no padrão do toggle ViewPoint).
   • GUIA DO SCHEMA — painel FIXO à esquerda (redimensionável) com TODAS as tags do dialeto YAML
     (code-schema.ts espelha domain.JobDefinition: id·label·team·jobType(13 tipos + WASM/K8S/GCP_RUN)·
     schedule(17 sub-tags)·params por tipo·upstream·retries·retryDelayMin·timeout·dryRun·confirm·
     selfService·environment·agentId·calendars·resources·conditionsIn/OutAdd/OutRemove·variables(tokens
     %%ODATE/EOM/…·%%SET/%%SETLOCAL)·sla·subWorkflow·actions On/Do), árvore collapsible ▸/▾ TUDO
     recolhido por padrão, formas aceitáveis explicadas, exemplos clique-pra-copiar, filtro por texto.
   • LINT AO VIVO — pausa de digitação (900ms) → dry-run no server (o MESMO validador do Aplicar: parse
     estrito + plano) → veredito no rodapé fixo, sem toast; typo de campo acusado na hora.
   • ERGONOMIA — Tab indenta / Shift+Tab desindenta / Enter mantém indentação (+2 após "chave:").
   ✗ DESCARTADO por decisão de escopo (item morto; se voltar, abre ID novo): editor Monaco/CodeMirror
     (dependência pesada × padrão zero-dep), diff lado-a-lado, split view canvas↔code, tabs por job,
     snippets, import/export de arquivo, git blame inline, modo código no Monitoring.
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

## 🚀 Features avançadas — ADV-1/2/3 *(entregues 2026-07-10)*

```
✅ ADV-1 — Schema DEDICADO por jobType (fonte única declarativa)
   • domain/typeschema.go: registry com os 16 tipos (COMMAND/SCRIPT/SSH/HTTP/DATABASE/FILE_WATCH/
     LAMBDA/BATCH/GLUE/STEP_FUNCTION/WAIT/CHOICE/PARALLEL/WASM/K8S/GCP_RUN) — campos, obrigatórios,
     kinds (string/int/bool/map/array/enum/scalar/intlist/json), enums, ALIASES de tipo (REST→HTTP,
     DB→DATABASE, …) e de campo (functionName→function) e regras cross-field (WAIT seconds|until,
     WASM path|url, PARALLEL branches≠∅). Campo DESCONHECIDO = erro citando o typo e os aceitos.
   • Validação em DUAS forças: DRAFT (save de session/jobs-as-code/bulk/massupdate — kinds+enums+
     campo desconhecido na hora, SEM cobrar obrigatórios: card recém-criado da palette não tem
     command/url ainda) × STRICT (publish + write direto + CLI test — obrigatórios cobrados; o
     publish devolve 422 estruturado listando job a job o que falta).
   • GET /api/jobtypes — catálogo read-only do registry (base p/ SDK/docs ADV-6/7).
   • DESALINHAMENTOS REAIS que o registry expôs e foram corrigidos nos executores: LAMBDA validava
     functionName mas o agente lia function (aceitos os dois, nos dois lados); payload da UI (objeto)
     era DESCARTADO pelo agente (agora string OU objeto); expectStatus escalar (200) e CSV da UI
     ("200,204") eram ignorados (agora int|lista|CSV); SSH port numérica era dropada; K8S insecureTLS
     aceita bool. Guia do CODE (code-schema.ts) atualizada — WASM/K8S/GCP_RUN ganharam card dedicado.
✅ ADV-2 — Multi-ambiente/multi-site: def.environment (F20) finalmente ROTEIA
   • hub.EnvMatch (regra única): lado sem label = coringa; ambos com label = tem que bater
     (case-insensitive). Setup sem -env segue idêntico; isolamento estrito = rotular os agentes.
   • Agente ganhou flag -env (REGENTE_AGENT_ENV) nos DOIS transportes (WS query + HTTP long-poll);
     PickAgent/HasAgent/Dispatch filtram por env; agente PINADO em env conflitante NÃO conta
     (misconfiguração ⇒ WAIT_AGENT com o motivo no Explain: "agente X está noutro ambiente").
   • Presença R5 (bus NATS) propaga o env — roteamento por ambiente vale CROSS-NÓ.
   • Visibilidade: /api/agents devolve environment (runtime, só online); chip ◉ env na tela de
     Agentes + linha no modal. Relaciona D-9 (promote Dev→Staging→Prod) e env_label do server (F20).
✅ ADV-3 — What-If / Statistics (completa o D-4 Forecast)
   • What-If: POST /api/whatif {date?, changes:[{defId, delayMin?, durationMs?, fail?, skip?}]} —
     scheduler/whatif.go projeta a diária DUAS vezes (baseline × cenário), função PURA no ethos do
     Forecast: elegibilidade pela MESMA IsScheduledOn, durações REAIS (p50 DayDurations → sla →
     5min), deps com a semântica do engine (AND por aresta; on-failure DESTRAVA recovery no cenário
     de falha; skip/bloqueio propagam rio abaixo), SLA deadline comparado nos dois mundos.
     Report: rows impactados-primeiro + summary (impacted/blocked/newSlaBreaches/makespan Δ).
   • WhatIfPanel no Monitoring (botão ao lado da Timeline): builder "E se <job> atrasar N min /
     durar N min / falhar / não rodar" → tabela baseline→cenário com Δ, efeito e SLA. Read-only.
   • Statistics: GET /api/analytics/jobstats?defId= (scheduler/jobstats.go) — janela das últimas
     200 execuções terminadas: runs/ok/notok, successRate, min/avg/p50/p90/max (SÓ sobre OK, como o
     D-4) e última execução. Aba Stats do drawer ganhou "Run statistics" acima do histórico D-4.
   • Testes: whatif_test.go (delay propaga, fail bloqueia+destrava recovery, skip, SLA novo,
     no-op) + jobstats_test.go + hub_env_test.go + bus env routing + typeschema_test.go.
```

## 📜 Changelog de entregas

> Log cronológico (mais recente primeiro) de tudo que foi entregue, com o "porquê" e os
> detalhes de implementação/validação. Complementa o §✅ Entregue (que agrupa por tópico).

| Quando | O que | Detalhe |
|----|--------|---------|
| ✅ | ~~**ADV-1 + ADV-2 + ADV-3: schema por jobType · roteamento por ambiente · What-If/Statistics**~~ | **Feito (2026-07-10):** as três primeiras Features Avançadas fecharam juntas. **ADV-1** — `domain/typeschema.go` = registry declarativo com o contrato de params dos 16 jobTypes (obrigatórios, kinds, enums, aliases de tipo E de campo, regras cross-field); validação em duas forças (DRAFT no save da session — typo/kind na hora, sem cobrar obrigatórios; STRICT no publish/write direto — 422 estruturado por job) + `GET /api/jobtypes`. Construir o registry expôs desalinhamentos REAIS validador×executor, todos corrigidos: LAMBDA `functionName`×`function` (cada lado lia um — aceitos os dois), payload objeto da UI descartado pelo agente, `expectStatus` escalar/CSV ignorado, `port` numérica do SSH dropada, `insecureTLS` só string. **ADV-2** — `def.environment` (F20, inerte desde sempre) agora ROTEIA: `hub.EnvMatch` (sem label = coringa; ambos com label = bate case-insensitive), flag `-env` no agente (WS + HTTP long-poll), PickAgent/HasAgent/Dispatch filtram, pin em env conflitante = WAIT_AGENT com motivo no Explain, presença R5 propaga env (vale cross-nó), chip ◉ na tela de Agentes. **ADV-3** — What-If: `POST /api/whatif` projeta a diária baseline×cenário (função pura estilo Forecast; mesma `IsScheduledOn`, p50 real do D-4, semântica de deps do engine — falha simulada BLOQUEIA on-success e DESTRAVA recovery on-failure) + `WhatIfPanel` no Monitoring; Statistics: `GET /api/analytics/jobstats` (runs/successRate/min/avg/p50/p90/max sobre OK + última execução) na aba Stats do drawer. Testes novos em typeschema/whatif/jobstats/hub_env/bus; suíte inteira verde (server+agent+CLIs), tsc+vite build ok. |
| ✅ | ~~**Refinamento UI: filtro do guia CODE só por TAG + cursor de pan nativo do SO**~~ | **Feito (2026-07-10, pedidos do usuário):** (1) o filtro do Guia do schema (modo CODE) casava tag+texto+formas — passou a casar **só o NOME da tag** (descendentes contam), placeholder "Filtrar por tag…". (2) Cursor do canvas: a 1ª tentativa (CSS `grab`/`grabbing`) ficou ruim de propósito documentar — grab/grabbing são **bitmaps internos do browser** (mão branca) que IGNORAM o tema de cursor do Windows, e o `cursor` no body perdia pro `pointer` dos cards. Solução final: **nada de mão no hover** (seta nativa) e, SÓ durante o arrasto, `body.canvas-panning` + regra `body.canvas-panning *{cursor:move!important}` — o **Mover NATIVO do SO** (respeita o tema de cursor do usuário, igual à transição seta→dedinho), vencendo o pointer dos cards ao atravessar folder. Um toggle de classe por pan — custo zero. Validado no preview (hover=auto, drag=move+classe, solta=limpa). |
| ✅ | ~~**CODE-1 (FECHA a trilha Jobs as code): modo código na linha LUXO (Matrix aposentado) + GUIA COMPLETA do schema + lint ao vivo**~~ | **Feito (2026-07-09):** o usuário mandou "matar o CODE-1": tirar o tema Matrix e levar o modo código pra linha luxo do Regente — e, no mesmo pedido, uma **"aba" FIXA à esquerda com TODAS as tags do YAML**. Entregue: **(1) Reskin luxo** — morreu o digital rain (canvas ~15fps), o verde fósforo #00ff41 e o "CARREGANDO O CONSTRUCTO"; o `CodeModeView` agora é linha luxo: título **serif itálico "Jobs as Code"** no accent do TEMA, fio gradiente sob o header (gesto do FolderManager), botões `.code-btn`/`.code-btn-solid` (padrão luxBtn), fundo/superfícies/tokens do tema, highlight YAML **tematizado** (chave = `--v2-accent-brand`, bool = `--v2-status-waiting`, número = `--v2-status-running`, comentário = muted; trocar o tema troca o código), caret accent; botão CODE da topbar virou `.code-toggle` no MESMO padrão do toggle ViewPoint (ativo = accent-deep + anel accent) — classes `.matrix-*` removidas do tokens.css. Zero cor de chrome hardcoded. **(2) Guia do schema (pedido do usuário)** — painel FIXO à esquerda, redimensionável (mesmo `useResizablePanel` da sidebar), com **TODAS as tags do dialeto** em árvore collapsible ▸/▾, **tudo recolhido por padrão**, cada tag com tipo/obrigatoriedade + formas aceitáveis explicadas uma a uma + exemplo YAML **clique-pra-copiar** + filtro por texto (filtro ativo força os achados abertos). Dados em `code-schema.ts`, espelho declarado do `domain/model.go` + `validate.go` + engines: id·label·team·**jobType com os 13 tipos implementados** (COMMAND/SCRIPT/SSH/HTTP/DATABASE/FILE_WATCH/LAMBDA/BATCH/GLUE/STEP_FUNCTION/WAIT/CHOICE/PARALLEL, params e obrigatórios de cada um + extras do agente WASM/K8S_JOB/GCP_RUN e aliases)·**schedule com as 17 sub-tags** (enabled/description/frequency/daysOfWeek/daysOfMonth/nthBusinessDays/monthsOfYear/advancedRule(4 regras)/shift/runAt/windowFrom·To/cyclic+intervalMin/cyclicMaxRuns/keepActive/cronExpression)·params(+`_agentId`)·upstream(4 conditions + default)·retries·retryDelayMin·timeout·dryRun·confirm·selfService·environment·agentId·calendar(legado)·calendars·resources·conditionsIn/OutAdd/OutRemove·**variables** (tokens nativos %%ODATE/ORDERDATE/RUNDATE/TIME/JOBNAME/JOBLABEL/FOLDER/INSTANCEID + %%EOM/BOM/EOY/BOY/NEXTBD/PREVBD/FIRSTBD/LASTBD, offsets ±N/±NB, %%SET/%%SETLOCAL, precedência)·sla·subWorkflow·**actions On/Do** (3 gatilhos × 4 ações, campo a campo) + entrada "— o documento" (multi-doc `---`, parse ESTRITO, delete-por-ausência, team inferível). **(3) Lint AO VIVO** — pausa de 900ms → `POST /code apply=false` (o MESMO validador do Aplicar) → rodapé fixo mostra plano/erros sem toast (`lintSeq` descarta resposta obsoleta); typo `jobტype` acusado na hora com a linha ("field not found in type domain.JobDefinition"). **(4) Ergonomia** — Tab indenta 2sp, Shift+Tab desindenta a linha, Enter mantém a indentação (+2 quando a linha abre bloco com `:`). **✗ Descartado por decisão de escopo** (item MORTO; se voltar, é ID novo): Monaco/CodeMirror (dep pesada × padrão zero-dep do projeto), diff lado-a-lado, split view canvas↔code, tabs por job, snippets, import/export, git blame, code no Monitoring read-only. **Validado AO VIVO E2E** (server GitOps em bare repo local + preview): guia nasce 100% recolhida e expande jobType→13 tipos+extras; lint pegou edição (`~atualizar 1`) e typo estrito sozinho; Aplicar persistiu (editor re-serializado do server com o valor novo, botão desarma); `.code-toggle` ativo computou accent do tema (rgb(17,199,111) + accent-deep), ZERO resquício matrix no DOM (sem canvas), zero erro de console; `tsc` + `vite build` verdes. |
| ✅ | ~~**UI-1 + UI-3 (FECHA o Refinamento UI do backlog): sidebar ACTIVE JOBS virtualizada até 1M (dual-mode windowed) + override de grade POR FOLDER (.regente-folder.yaml)**~~ | **Feito (2026-07-09):** os 2 últimos itens de Refinamento UI do §🔜 Backlog. **(1) UI-1 — a lista mostra o DIA INTEIRO, validado AO VIVO com 1.000.000 de jobs seedados** (`tools/seed -n 1000000 -folders 200`): a `MonitoringSidebarV2` foi reescrita com **virtualização real** (headers de folder + rows calculados por offset; DOM = só o visível, medido: **37 rows no DOM p/ 1M**) e ficou **DUAL-MODE** — dia ≤ cap: dados do espelho local, filtro/busca client-side, comportamento de sempre; dia > cap (decidido pelo `total` REAL do `/summary`, não pelo tamanho do espelho capado): **windowed server-driven no MESMO padrão do ScaleMonitor** — estrutura/contagens de `/api/instances/summary` (pills com números do DIA: 125.000 FAIL etc.), linhas por página de `/api/instances/page` com **`offset` NOVO no server** (random-access: salto de scrollbar pro meio do dia pede a página N direto, sem andar 3k cursores; keyset/cursor preservado e vence se ambos vierem), placeholders `···` enquanto a janela carrega, filtro por status e busca (`q`) **reescopados NO server** (viewSummary), updates ao vivo: `instance.changed` atualiza a row em cache in-place + summary re-lido com throttle 2,5s. **Altura virtual comprimida por fator acima de 12M px** (1M×30px = 30M px estoura o teto de altura do browser ~17M Firefox e a precisão float32 ~16,7M; scrollbar continua representando o total; layer sticky posiciona rows relativas ao viewport). **Contador honesto**: header = "N de <total real>" (ex.: "1.000.000 de 1.000.000"; filtrado: "125.000 de 1.000.000") — nunca número truncado disfarçado de total; + aviso "lista: dia inteiro · grafo: primeiros <cap>" com botão **ViewPoint** (abre o ScaleMonitor). **Clique em row fora do espelho** (dia > cap): `fetchInstanceById` puxa a instance pro cache pelo `q` do listInstances (match exato client-side) → o drawer abre normalmente (provado no job-0999998). **Cap do canvas virou configurável** (Settings → "Cap do grafo", `regente:legacyCap` 500–5000 = teto do parseLimit do server; default 2000): o store lê na hora do fetch e o evento `regente:layout-changed` dispara **re-fetch ao vivo** (novo `refreshInstancesFromServer` no runtime-bridge — o listener antigo só relia snapshot). O eye do FolderManager (visibleFolders) vale nos DOIS modos. **(2) UI-3 — override de `columns`/`maxRows` POR FOLDER**, persistido no stub **`.regente-folder.yaml`** (a decisão viaja com o workspace Git — versionada/PR-ável — e NÃO é pref de browser): storage ganhou `FolderLayout` + `folderMeta` (parse do stub; stub antigo só-marker segue compatível) + `SetFolderLayout` (preserva `name`, commit no mesmo caminho dos saves); `PUT /api/folders/{name}/layout` (validação 0–40/0–200, `{}` remove o override, `requireFolderWrite`, directPush + broadcast `folder.changed`) e `GET /api/folders` devolve `layout`. Front: `resolveLayout(team) = override ?? global` aplicado em `composeColumns` (**Monitoring E Design**); `V2Preview` carrega os layouts 1× e re-carrega no `folder.changed`/`_connected` → **o canvas re-grida AO VIVO** (validado: GRID 14 soltos, 10 colunas default → salvei 4 → 4×4 sem F5); editor no card do FolderManager (ícone ⊞ no hover-act: 2 inputs numéricos, **vazio = herda o global**, ambos vazios = limpa; badge **"⊞ 4×·"** no card quando há override). **Testes:** `TestFolderLayout_RoundTrip` (set→list, parcial, nil limpa, folder inexistente erra, stub não conta como job) · `TestSetFolderLayout_API` (PUT+GET, 400 fora do range, `{}` limpa) · `TestPageInstances_Offset` (offset pagina certo, além-do-fim vazio, cursor vence offset) + suíte Go completa + `tsc`/`vite build` verdes. **Validação AO VIVO completa** (server real + seed): 1M → header/aviso/pills/DOM-37-rows/scroll-meio-do-dia-carrega-a-página-certa/FAIL 125k exatos/busca "1 de 1.000.000"/drawer do job-0999998; re-seed 414 → modo local intacto ("414/414"), grade 10→4 colunas ao vivo, `.regente-folder.yaml` com `layout.columns: 4` no disco; cap 500/900 honrado no fetch inicial E no evento sem F5; zero erro de console. |
| ✅ | ~~**Refinamento UI: abas do drawer do Monitoring padronizadas com o Design + card violeta p/ "Wait for confirmation"**~~ *(rotulada "UI-3" no commit d04f0cd por engano — UI-3 do §Backlog é o override de grade por folder, entregue na linha acima)* | **Feito (2026-07-09, commit f77077e):** dois pedidos do usuário, só o VISUAL/UX (sem tocar ações/funcionamento). **(1) Padronização das abas** — as abas do `InstanceDetailsDrawer` (Monitoring) usavam pills com realce laranja (`.v2-tabrail`/`.v2-tab`/`.v2-tab-active`, o "círculo laranja"); passaram a usar o MESMO sublinhado fino do `JobConfigDrawer` (Design): inline `borderBottom:2px solid var(--v2-accent-brand)` na ativa, transparente nas outras, font mono, `borderRadius:0`, contador de deps vira ` (N)` inline. Abas e conteúdo idênticos; as classes `.v2-tab*` em `tokens.css` viraram CSS morto. **(2) Card "Confirm"** — job WAITING preso no gate Control-M "Wait for confirmation" (`def.confirm && !confirmed`) agora fica **todo violeta** (`#a78bfa`: fundo `linear-gradient` composto sobre a surface + borda + glow) com **tag `✋CONFIRM`** e status lendo **"CONFIRM"**. Novo flag `JobNodeData.waitConfirm`, derivado em `canvas-layout.ts` (buildMonitoringCanvas) no MESMO padrão do `waitAgent` — lê a def viva de propósito, batendo com o gate real do server; **não fere a imutabilidade do card** (é bloqueio de execução, como waitAgent/waitEvent, não snapshot tipo dryRun). A seleção NEON continua vencendo o violeta. **Botão direito** no card ganhou **"Confirmar"** (primary, 1º item) quando o gate está ativo → `confirmInstance` (endpoint POST /instances/{id}/confirm, que já existia e é usado pelo botão do drawer); ao confirmar o card sai do violeta e o job executa. Plumbing já existia (`def.confirm`/`inst.confirmed` já chegavam no front via `ServerApiAdapter.toWeb`/`server-instance-store.toWeb`). `tsc` + `vite build` verdes; **validado AO VIVO** no preview (computed styles: aba ativa `2px solid accent`/radius 0/mono = Design; card com bg gradient violeta + borda `rgb(167,139,250)` + tag + menu "Confirmar"; e limpeza do violeta ao confirmar → job roda a OK). |
| ✅ | ~~**UI-2: drawer de detalhe do job (Monitoring) redesenhado — 2 abas → 8 abas em pill de luxo**~~ | **Feito (2026-07-09):** report do usuário — a "janelinha" que abre ao clicar num job no Monitoring (`InstanceDetailsDrawer`) tinha só 2 abas (details/log) e empilhava tudo numa coluna. Redesenhada em **8 abas nomeadas em inglês** com botões em **pill de luxo** tematizados (novas classes `.v2-tabrail`/`.v2-tab`/`.v2-tab-active` em `tokens.css`: pílulas arredondadas, `--v2-accent-deep`+glow no ativo, trilho com scroll-x e máscara de fade nas bordas, contador no pill de Dependencies): **General** (Job details — Job Name·Application[placeholder "not configured yet"]·Job Type·Folder·Environment · **Description** · **Timeline** · **Parameters** = variáveis snapshotadas na instance ∪ `localVars` F18 da def, em caixas key/value · **Execution config** = actionConfig visível) · **Output** (error + output JSON) · **Logs** (LogPanel intacto) · **Statistics** (métricas desta run + histórico de duração p50/p90/next via `/api/analytics/forecast`, com fallback sem histórico) · **Schedule** (tradução **read-only** do schedule de DESENHO: `describeFrequency` traduz daily/weekly[dias]/monthly/businessday[N-ésimo]/advanced, + meses·roll·timezone·run-at·janela from→to·cyclic·keep-active·calendars include/exclude) · **Dependencies** (Depends-on/Triggers com labels do `allDefs` + **Resources** F15 ×qtd + **Conditions** IN/OUT+/OUT− F16 + **SLA** F19 + **Sub-workflow** F17) · **Neighborhood** (`NeighborhoodPanel` auto-load ±raio + Blast) · **Why not?** (Explain + RCA; fallback claro em modo local). **Habilitou o dado que faltava** ("criamos parâmetros/recursos mas não tinha onde ver"): o `ServerApiAdapter.toWeb` **dropava** `schedule.description`/`resources`/`conditionsIn·OutAdd·OutRemove`/`variables`(F18→`localVars`)/`environment`/`sla`/`subWorkflow` — agora carrega TODOS e faz **round-trip no `toServer`** (corrige perda silenciosa: editar+salvar no Design um job vindo de YAML/import apagava esses campos). Tipos TS estendidos (`JobDefinition`). Também corrigido `--v2-bg-deep` (token que nunca existiu → transparente) para `--v2-bg-canvas`. V2Preview passa `definition`+`allDefs` ao drawer. `tsc` verde; **validado AO VIVO** (def+instance ricas semeadas: as 8 abas renderizando o conteúdo certo, pills tematizados, zero erro de console). **PENDENTE** (próximo passo, sinalizado ao usuário): editor de Parameters/Description/Application no **Design** — Monitoring é imutável, mostra read-only; Application ainda não tem campo no backend (placeholder pronto). **Follow-up mesmo dia (report do usuário):** (1) **Job Type dessincronizado** — o drawer recebia a instância CRUA (`selectedInstance`), enquanto os cards do canvas usam `instanceToMonitoring(enriched)` com fallback da def; agora o `V2Preview` enriquece a instância passada ao drawer do mesmo jeito (jobType/label/team caem pra def se vierem vazios) → drawer = card. (2) **Seção Action** (o que o job FAZ) que estava diluída no rodapé da "Execution config" virou **seção própria logo abaixo de Description**, sincronizada por jobType (mapa `ACTION_FIELDS`/`ACTION_VERB` espelhando o `JobActionConfigEditor`: COMMAND→command/cwd, HTTP→method/url/headers/body/status, DATABASE→driver/dsn/sql, SSH/SCRIPT/FILE_WATCH/LAMBDA/BATCH/GLUE/STEP_FUNCTION/WAIT/CHOICE/PARALLEL + Agent; chaves fora do schema caem em "Other" pra nada sumir). (3) auditoria: removido o campo **timezone** do Schedule (não há tz por-job no backend — só global na daily E1; era vestígio só-front) e o dump cru de actionConfig da Execution config. Validado ao vivo (3 jobs COMMAND/HTTP/DATABASE + instância de jobType vazio → tudo sincronizado). |
| ✅ | ~~**MCP-1: writes RICOS via MCP (2→11 tools de escrita) + tool `forecast` + docs**~~ | **Feito (2026-07-08):** a camada agent-native (`server/cmd/mcp`) tinha só 2 writes (rerun/set_ok); agora expõe **11 tools de escrita**, todas gated por `-allow-writes` + aprovação do cliente e marcadas `destructiveHint`, cada uma fachada de 1 endpoint REST JÁ existente (não toca o core): **unitárias** hold/release/cancel/confirm/rerun/set_ok (`POST /api/instances/{id}/…`), **force_order** (`POST /api/definitions/{id}/force` — roda uma DEFINITION agora, devolve o instanceId), **pause_folder/resume_folder** (`POST /api/folders/{name}/pause|resume` — WAITING↔HELD em massa, estado preservado), **bulk_action** (`POST /api/bulk/instances` {action, ids[]} — transacional por item, máx 500) e **ingest_event** (`POST /api/events/ingest` — seta conditions[] e/ou força um job, idempotente pelo id do emissor). Refactor pra não repetir boilerplate: `gate()` central (nega sem -allow-writes), `writeID(action)` (gate+instanceId+POST) pras 6 unitárias, `strSlice()` (aceita array JSON, []string ou CSV) pros args de lista (ids/conditions). Adicionada tb a tool de LEITURA **`forecast`** (`/api/forecast`; `days>1` → `/api/forecast/range`, a janela ≥1 semana da entrega anterior). Total: **22 tools = 11 read + 11 write** (o read já tinha crescido pros 4 diferenciais — a doc estava stale em "6 read"). **Validado AO VIVO** pelo binário real (pipe JSON-RPC): sem flag = 11 tools e todo write responde "escrita desabilitada"; com `-allow-writes` = 22 tools; cada write bate no endpoint+verbo certo (asserção por echo do método+path no mock). **7 testes Go** (novos: `TestMCP_RichWrites` cobrindo gate+endpoint+args-obrigatórios das 11 · `TestMCP_ForecastTool` days=1 vs 7; atualizado o list-test pros 11+11). `docs/mcp.md` reescrito (tabelas 11 read/11 write + exemplos de prompt de escrita) e README (bloco MCP "22 tools" + linhas novas na tabela de API: set-ok/confirm/bulk/forecast/forecast-range/pause-resume/ingest). |
| ✅ | ~~**CTM-4/5/6: baterias EXAUSTIVAS do Aprofundamento Control-M (calendários · recursos · Forecast ≥1 semana) + fix da divergência do Forecast**~~ | **Feito (2026-07-08):** fecha os 3 itens de VALIDAÇÃO residual do roadmap (as features já existiam; faltava cobrir densamente e corrigir onde o gating divergia). **Divergência achada e corrigida:** `scheduler.Forecast()` decidia elegibilidade por `Enabled` + o calendar LEGADO (`d.Calendar` via `IsEligibleDate`) e **ignorava frequência/monthsOfYear/multi-calendar/shift** — um job "só segundas" aparecia elegível TODO dia no painel de Forecast, contradizendo o que a daily de fato materializava. Agora o Forecast usa `IsScheduledOn` (a MESMA fonte única do `RunDaily`), com um `mapCalLookup` adaptando o mapa nome→calendar ao contrato do `calStore` e um `scheduleReason` explicando a ausência ao operador. **Novo capability `ForecastRange(defs, cals, from, days)`** (pure, clamp [1,366]) + rota `GET /api/forecast/range?from=&days=` (default 7) — o "≥1 semana à frente" do Control-M virou de verdade multi-dia, cada dia pelo gating real. **CTM-4 (calendários):** 1º dia útil (`advanced:first-businessday`, com/sem feriado deslocando) · só segundas (`weekly[mon]`) · 1º dia útil que NÃO é segunda (`first-businessday-not-monday`: julho começa qua→01, junho começa seg→02, com feriado→03) · N-ésimo dia útil (5º=07, múltiplos [1,-1]) · include/exclude/feriados no nível do `IsScheduledOn` · monthsOfYear (isolado e combinado com weekly) · **guard de divergência**: os dois caminhos de "dia útil especial" (`advanced` vs `businessday`+NthBusinessDays) têm que concordar dia-a-dia por TODO julho (first/last/penultimate). **CTM-5 (recursos):** lock exclusivo · recurso desconhecido nasce cap 1 (lock implícito) · máximo simultâneo por pool (N/N → o (N+1) entra na FILA via `Shortfalls`, a mesma função do gate `WAIT_RESOURCE`; libera → sai da fila) · quantitative multi-unidade + all-or-nothing (pedido que não cabe NÃO reserva parcial) · liberação correta multi-detentor/multi-recurso + idempotente + no-op em instance inexistente · reduzir capacidade abaixo do uso (over-subscription não altera uso, barra novos). **CTM-6 (Forecast ≥1 semana):** o fix vira regression-guard (só-segundas não é elegível no domingo) · a semana inteira batida contra um **oráculo escrito à mão** (independente do IsScheduledOn) numa carga mista (diário·monfri·mononly·firstbd·disabled) · range respeita feriado/fim-de-semana · ondas topológicas A→B→C=0/1/2 em cada dia · pico de recursos por onda mais carregada · clamps + data inválida · fronteira order-time × runtime documentada (conditions são gate de runtime — `gateInstance`, não de ordenação — então job com condition pendente AINDA aparece no forecast). **20 testes Go novos** (`internal/scheduler/ctm_batteries_test.go`) + `go vet ./...` + suíte completa do server verdes. |
| ✅ | ~~**Fix (seleção): clique 1× já seleciona + clique simples troca (single-select), fim do "2 cliques" e do acúmulo A+B**~~ | **Feito (2026-07-08):** follow-up do report do usuário logo após o highlight neon — (a) 1 clique num job NÃO acendia, só no 2º; (b) clicar no job A e depois no B deixava **os dois** highlighted (deveria trocar). Raiz: a seleção NATIVA do ReactFlow brigava com o prop `nodes` CONTROLADO (o `displayNodes` reasserta `selected` do `selectedIds`, que só era escrito pelo `onSelectionChange` do RF, com lag/eco → off-by-one + acúmulo). Fix = **onNodeClick vira AUTORITATIVO** da seleção: clique simples = seleção ÚNICA (`new Set([bare])`, troca), Shift/Ctrl/Cmd = alterna (multi, casando com o multiSelectionKeyCode e o rubber-band); lanes não selecionam. O `onSelectionChange` foi **gated** pra escrever só durante rubber-band (Shift+arrasto, marcado por `onSelectionStart`/`onSelectionEnd`) — o eco do clique não clobbera mais o selectedIds; e **`onPaneClick` limpa** a seleção ao clicar no vazio. Como o `displayNodes` já força o `selected` do RF a cada render, o estado final é sempre = selectedIds (determinístico). Validado AO VIVO (preview): 1 clique acende na hora (borda accent-brand + glow 16px), Shift+clique alterna off, clique no vazio limpa; `tsc -b` + `vite build` verdes. |
| ✅ | ~~**Refinamento UI: highlight NEON do(s) job(s) selecionado(s) (Design + Monitoring) + fim do "azulzão" de multi-seleção**~~ | **Feito (2026-07-08):** report do usuário — ao selecionar 1 job não dava pra saber QUAL estava selecionado (borda `--v2-accent-dark`, escura demais), e selecionar mais de 1 pintava um **grande retângulo azul arrastado "tipo Windows" sobre a área inteira**, não nos jobs. Três mudanças: (1) **halo NEON por card** — `JobNodeV2`, quando `selected`, ganha borda `--v2-accent-brand` + `box-shadow` de anel 1px + glow 16px na cor do tema (`--v2-accent-glow`), preservando o inset de grain; vale pra 1 ou N jobs (cada card selecionado acende sozinho) e nos DOIS modos (usam o `selected` nativo do ReactFlow). (2) **fim do "azulzão"** — o ReactFlow desenha por padrão um bounding box azul (`.react-flow__nodesselection-rect`) em volta de TODOS os nós selecionados; neutralizado em `index.css` (background transparente + `border:none` — a caixa continua existindo pra permitir arrastar o grupo junto, só fica invisível); o retângulo de rubber-band (Shift+arrasto) foi **tematizado** (`.react-flow__selection` = glow do tema + borda accent-brand) no lugar do azul cru. (3) **seleção PERSISTE entre rebuilds** (raiz descoberta na validação) — `nodes` é prop CONTROLADO e o canvas é rebuildado a cada tick do Monitoring / ao abrir o drawer no Design; a seleção interna do RF era ZERADA no rebuild (o highlight piscava e sumia). Fix: `displayNodes` reaplica `selected` a partir do `selectedIds` (fonte da verdade, alimentada pelo `onSelectionChange`) sobre `canvas.nodes` num memo leve — geometria/câmera continuam em `canvas.nodes` (selecionar **não** mexe na câmera). Validado AO VIVO (preview): card selecionado com borda `rgb(17,199,111)` + glow 16px + anel 1px, "1 SELECTED" na bulk bar, seleção sobrevive a rebuild forçado (`regente:layout-changed`), `.react-flow__nodesselection-rect` computa transparente/sem borda e `.react-flow__selection` na cor do tema; `tsc -b` verde, zero erro no console. |
| ✅ | ~~**Diferenciais além do Control-M — FECHA a trilha (D-1..D-15): orquestração híbrida/stateful · DevEx (DSL + `regente test`/`dev`/`promote`) · policy-as-code · chaos · "wow" (Gantt · templates · portal · quick-actions)**~~ | **Feito (2026-07-08).** Fecha os 15 diferenciais em aberto, em 3 levas, tudo com teste Go/TS + validação AO VIVO (server demo + preview). **Leva A — orquestração & enterprise (backend):** (1) **D-1 human-in-the-loop/long-running** — `def.retryDelayMin>0` agenda o retry por `scheduled_at` DURÁVEL em vez de goroutine dormindo (sobrevive a restart/deploy); o carry-over passou a tratar `WAITING com attempts>1` como NOTOK-em-tratamento (`carryDecision` ganhou o parâmetro `attempts`), então um "retry após 3 dias" atravessa as viradas da daily; compõe com Confirm/keepActive. (2) **D-2 pause/resume de WORKFLOW** — `POST /api/folders/{name}/pause|resume` (`api/workflow.go`): WAITING↔HELD em massa **set-based** (1 UPDATE + INSERT…SELECT de eventos numa transação — pausar 50k jobs não é 50k round-trips), attempts/cycle_runs/scheduled_at/confirmed **intactos** (é pausa, não reset), RUNNING não é pausável, carry-over persiste HELD entre diárias (folder pausada por dias). (3) **D-3 event-driven confiável** — `POST /api/events/ingest` (`api/ingest.go`): evento externo IDEMPOTENTE (dedupe pela PK `id` do emissor — retry at-least-once responde `duplicate:true` sem re-aplicar) seta conditions e/ou force-ordena e cutuca o tick (destrava WAIT_CONDITION sem polling); `external_events` (migration **v13**) guarda o payload + `applied` p/ forense. (4) **D-5 query estruturada** — `POST /api/instances/query` (`api/instquery.go`): filtro COMPOSTO e TIPADO (ranges de data, IN de folders/statuses, flags forced/carried/late, groupBy, keyset cursor), parse **estrito** (campo desconhecido = 400), + o método HTTP **`QUERY`** (draft IETF, safe+idempotente com body) na MESMA handler (`chi.RegisterMethod`) — a decisão de transporte registrada em 2026-06-24, agora implementada. (5) **D-10 policy-as-code** — `pkg/policy` lê `policies.yaml` da RAIZ do workspace (versionado junto dos jobs): regras requireSLA/requireRetries/requireDescription/requireCalendar/idPattern/allowedJobTypes/maxRetries/forbidDryRun, enforcement error|warn|off, exemptFolders; **gate do publish** (session valida contra o policies.yaml do PRÓPRIO clone → 422 estruturado; warn publica com warnings), feedback no modo CODE, `GET /api/policy`; política quebrada (YAML/regex inválidos) BLOQUEIA (não falha aberto). (6) **D-11 chaos** — `POST /api/instances/{id}/inject-failure` (`scheduler/chaos.go`): falha SINTÉTICA pelo `FinishInstance` REAL (retry/On-Do/alerting reagem como numa falha orgânica), só WAITING/RUNNING/HELD, evento `chaos` auditável. **Leva B — DevEx/CLI (`server/cmd/regente` + `server/pkg/jobdsl`):** (7) **D-6 schedule-as-code** — pacote `jobdsl`: builder fluente Go (`w.Folder("fin").Job(...).Command(...).After(extract).SLA(...)`) → YAML **byte-compatível** com o workspace, `Validate()` (id dup/label/dep órfã), output determinístico; golden + example test. (8) **D-7 `regente test <job.yaml|ws>`** — parse ESTRITO + validação estrutural + grafo (deps órfãs/**ciclos** por DFS) + **policy** + **simulação com o engine REAL** (reusa `scheduler.DryRun`/`IsScheduledOn` — a MESMA decisão da daily): RUN/WAIT/BLOCKED por job; exit 1 no CI, `-json`. (9) **D-8 `regente dev daily`** — server local descartável (SQLite temp, demo-mode, daily materializada no boot, engines Control-M ligados) numa porta — loop editar→testar→ver rodar sem tocar produção. (10) **D-9 `regente promote -from -to`** — promoção multi-ambiente Git-NATIVA (ambientes = branches): clona o destino, faz o snapshot da origem (definitions+calendars+policies.yaml — código E política juntos) SUBSTITUIR o destino (add/update/**delete**, não merge), diff + `-dry-run`, commit+push; o server do ambiente pega pelo GitOps normal. Fix de carona: o `flag` do Go parava no 1º posicional → `-json`/`-date` após o path eram ignorados; `reorderArgs` move posicionais pro fim (flags em qualquer ordem). **Leva C — UI/wow (front + endpoints de apoio):** (11) **D-4 performance forecasting** — `scheduler/perfforecast.go` (`GET /api/analytics/forecast`): p50/p90 + tendência por regressão linear + previsão da próxima + **ETA do RUNNING**, a partir do histórico OK; `/api/analytics/durations` dá p50 por def p/ a Timeline; `ForecastPanel` desenha sparkline SVG puro no drawer (some com <2 execuções). (12) **D-12 Visual Schedule Editor** — `TimelineView`: **Gantt da daily** sobreposto ao Monitoring, uma linha por job agrupada por folder, régua 00–24h, **barras reais** (started→finished/agora) e **previstas** (p50, hachura), linha do "agora", SVG puro (cap 400 linhas). (13) **D-13 templates** — tabela `job_templates` (migration v13) + `api/wow.go` CRUD; "☆ Template" no JobConfigDrawer salva a FORMA (server descarta id/team/upstream) + aba **Templates** na palette do Design instancia um job novo a partir do molde. (14) **D-14 self-service portal** — `def.selfService:true` + rota **`/portal`** (`PortalView`, mobile-friendly): só jobs expostos, botão Rodar, gate PRÓPRIO (`api/wow.go` — qualquer logado, opt-in versionado no YAML; job não-exposto responde 404, não vaza catálogo). (15) **D-15 mobile-friendly alerts** — `pkg/quickaction`: tokens HMAC-SHA256 de escopo ÚNICO (instance+action+exp, TTL 24h, allowlist sem cancel) anexados aos alertas Slack/webhook; rota pública **`GET/POST /qa/{token}`** (`api/wow.go`) — GET mostra página de confirmação (preview de link não executa), POST executa; secret auto-gerado em settings. Validado AO VIVO: `regente test` pegou violação de policy (exit 1) + simulação 1RUN/1WAIT/1BLOCKED; ingest idempotente (applied→duplicate); QUERY verb; pause/resume; chaos (400 em OK, injected em RUNNING); self-service (lista+run, 404 no privado); quick-action (403 HTML em token ruim); forecast/durations/templates; UI com botão Timeline + Gantt (overlay 1380×747, 24h, 36 rows), botões ⏸/▶ por folder, `/portal` renderizado. **~30 testes Go/TS novos** (policy·quickaction·jobdsl·cmd/regente·scheduler retry-delay+forecast·api differentials2+wow) + `go vet ./...` + suíte completa + `tsc --noEmit` + `vite build` verdes. Com isto a trilha **Diferenciais fecha 100%** — resta só a Fase Z (divulgação). |
| ✅ | ~~**Backlog Enterprise E4+E5+E6 (FECHA a trilha E1..E6): fila assíncrona de eventos · relatório/SLO da daily · importador Control-M**~~ | **Feito (2026-07-07):** (1) **E4 fila assíncrona de eventos** — `scheduler/eventqueue.go`: canal buffered (cap 10k) + goroutine writer única que grava em LOTE (INSERT multi-values; flush a cada 250ms OU 500 eventos — espírito do insertDailyBatch); `emitEvent` vira send não-bloqueante e **fila cheia degrada pro INSERT síncrono (nunca perde)**; ordem POR INSTANCE preservada no caminho da fila (canal FIFO único + writer único); **flush final no shutdown** (Stop() drena o canal — main agora chama sched.Stop() no SIGTERM; writer rastreado por quit+wg, zero goroutine órfã = sem flake do TempDir); métrica `regente_event_queue_depth` no /metrics; **opt-in por StartEventQueue()** — ligada no modo `-scheduler=internal`, DESLIGADA no external/serverless de propósito (scale-to-zero congela o processo e mataria eventos bufferizados; síncrono é o correto lá) e nos testes (determinismo). Testes: 10k emits concorrentes → zero perdas + ordem por instance; fila-cheia determinística (canal sem writer); lotes pequenos; flush no Stop. (2) **E5 relatório/SLO da daily** — `GET /api/daily/report?date=` (`scheduler/dailyreport.go`): counts {ordered/ok/notok/waiting/running/held/cancelled/carried} em 1 query agregada + `startedAt` do daily_runs + **`lateStart`** (started_at > daily_at+5min NO relógio de negócio E1) + `closed` + failures (NOTOK, cap 100, defId/team/exitCode/finishedAt) + slaBreaches (join sla_breaches × instances do dia) + `reportSent`. **Push opcional**: setting `daily_report_channels` (CSV slack/webhook/email/pagerduty — REUSA os sinks do alerting via `FireAction`, então cai no sino da UI + canais externos) enviado quando a daily **FECHA** (zero WAITING/RUNNING; check no tick com throttle 1min, leader-only) OU no horário `daily_report_at` (fallback p/ dias que nunca fecham, ex. cyclic); **idempotente por claim** `UPDATE daily_runs SET report_sent_at WHERE report_sent_at IS NULL` (migration **v12**) — 1 envio por diária mesmo multi-nó. UI: **card compacto no topo do Monitoring** (`DailyReportCard`: pill flutuante "DAILY <date> · N ok · N notok · N abertos · ⏰ atrasada · fechada ✓ report") com os números DO ENDPOINT (não recalcula; refresh no mount + daily.started/instance.bulk + 60s; só no board clássico — ScaleMonitor tem dashboard próprio). Testes: agregado exato multi-status; lateStart na tz de SP; envio idempotente; fallback por horário. (3) **E6 importador Control-M** — binário `server/cmd/importctm` (pure-Go, zero dep nova): lê XML de export (`DEFTABLE`/`FOLDER`/`SMART_FOLDER`/`JOB`) e gera workspace local (`definitions/<folder>/<job>.yaml` no dialeto EXATO do FileStore + `calendars/*.yaml` stubs + `import-report.md`). Mapeamentos v1 TODOS documentados no README do cmd: JOBNAME→id slug · DESCRIPTION→label · PARENT_FOLDER→team · TASKTYPE Job/Command→COMMAND(CMDLINE→params.command) · Dummy→COMMAND dryRun · FileWatcher→FILE_WATCH(path) · TIMEFROM/TIMETO→runAt/windowTo · WEEKDAYS→weekly · DAYS(1,15,L)→monthly(-1) · DAYSCAL/WEEKSCAL/CONFCAL→calendars include (dedupe) + SHIFT >/< → next/prev-businessday · **INCOND→upstream quando o OUTCOND tem 1 emissor; senão conditionsIn F16 com o MESMO nome** (OUTCOND+ de cond virada aresta é omitido; SIGN "-"→conditionsOutRemove) · SHOUT OK/NOTOK→actions notify (URGENCY V/U/R→critical/warning/info) · MAXRERUN→retries · CYCLIC+INTERVAL(M/H/D)→cyclic/intervalMin · %%VARIABLE→variables. SUB_APPLICATION/DATACENTER/RUN_AS/NODEID/…→ignorados COM AVISO; **qualquer atributo desconhecido → `# TODO-import:` no YAML + coluna Pendências no relatório** (nada se perde em silêncio); `-dry-run` não escreve NADA; `-folder-filter`; NUNCA push. Golden test com fixture cobrindo cada mapeamento + dry-run + filter. **Validado AO VIVO E2E**: importctm → workspace do server offline → Run Daily ordenou os 7 jobs importados (upstream extract_fin→load_fin funcionando), /api/daily/report refletiu 9/7/2 com lateStart, push chegou 1× no sino/alert_events e reportSent travou, card na UI com os números do endpoint, `regente_event_queue_depth` no /metrics e os eventos da daily fluindo pela fila. **13 testes Go novos** (4 eventqueue · 4 dailyreport · 3 importctm+2 sub-asserts) + verify.sh (server/agent/app) verde. |
| ✅ | ~~**Backlog Enterprise E1+E2+E3: timezone da daily · auditoria (retenção+export+settings) · RBAC operacional folder-scoped**~~ | **Feito (2026-07-07):** (1) **E1 timezone da daily** — setting `daily_timezone` (nome IANA; vazio = relógio local): `autoDailyIfDue` decide `today`/horário-alvo pelo relógio de **NEGÓCIO** (`NowLocal()` = `nowFn().In(loc)`; `nowFn` injetável p/ teste), o `order_date` gravado é o dia NESSA timezone (server UTC + negócio SP cruza a meia-noite às 03:00Z), o `POST /api/daily/run` manual usa a MESMA régua (`TodayDate()`), e `GET /api/daily/status` ganhou `timezone` + `orderDate`/`serverNow` na tz de negócio. `LoadLocation` cacheado por nome (mudou o setting → recarrega sem restart; inválido loga 1× e cai no local) e base IANA **embutida no binário** (`import _ "time/tzdata"` — Windows/scratch não têm zoneinfo). UI: campo "Timezone da daily" ao lado do horário em Settings→Geral com datalist de sugestões. Validado AO VIVO: tz vazia = local (-03:00), `Pacific/Kiritimati` (UTC+14) virou `orderDate` pro dia seguinte, inválida caiu no local. (2) **E2 auditoria enterprise** — (a) trilha **PERSISTIDA**: tabela `audit_events` (migration **v11** SQLite+PG); todo `s.audit()` (login, definition.write, settings.write) grava nela além do sink SIEM; (b) **retenção**: setting `audit_retention_days` (0/vazio = infinito) → GC diário de `instance_events`+`audit_events` logo após a daily, só no líder, em lotes de 10k (`auditgc.go`; DELETE dialect-safe com corte calculado pelo PRÓPRIO banco — `datetime('now')`/`now()-make_interval` — formatos de DATETIME diferem entre SQLite e PG), síncrono de propósito (goroutine solta = flake do TempDir); (c) **audit de settings**: `PUT /api/settings` emite `settings.write` com a lista de chaves REALMENTE alteradas e `de→para` — segredos (lista única `secretSettingKeys`, a mesma do GET mascarado) aparecem só como "(alterado)"; flags sintéticas `*_set` que a UI devolve no PUT são ignoradas (não viram linha nem evento; achado na validação ao vivo) e no-op não audita; (d) **export**: `GET /api/audit/export?from=&to=&after_id=&limit=&format=jsonl` (admin-only, streaming NDJSON, teto 100k linhas/chamada) unifica as duas fontes `{cursor, source, ts, kind, actor, instanceId?, detail…}` com **cursor estável por linha** (`i:<id>`/`a:<id>` — retomar = after_id da última linha; fontes em sequência, cada uma por id; from/to dialect-safe via `tsArg`). (3) **E3 RBAC operacional** — `instanceFolder` agora resolve pela coluna **`team` snapshotada da instance** (1 SELECT; fallback def viva só p/ instances pré-v4 — antes era `Store.List()` a CADA item do bulk) e hold/release/cancel/rerun/set-ok/confirm/bulk exigem `CanWriteFolder` na folder dona além do writer role (force já exigia); job **solto** (team='') passa pelo MESMO `CanWriteFolder("")`: admin/operator irrestrito podem, user em modo ACL-restrito não (coerente com o read-path, que nem lista team='' pra ele; antes era admin-only), e o bulk reporta **403 POR ITEM** sem abortar o lote; bearer legado segue admin (bypass by design). Validado AO VIVO: operator ACL write=[FIN] → rerun FIN 200, RISCO 403 ("no write access to folder"), bulk misto ok=1/failed=1 citando a folder, admin legado 200. **11 testes Go novos** (scheduler: 5 — tz que cruza meia-noite de SP/TodayDate/fallback+cache de tz/GC com lotes/GC desligado · api: 6 — RBAC unitário+hold+bulk por item, settings sem vazar segredo+no-op+flags `_set`, export paginado+admin-only) + `scripts/verify.sh` (server build/vet/test · agent · app build) verde. |
| ✅ | ~~**Refinamento UI: câmera é do usuário (fim da centralização sozinha) + limites de pan por conteúdo**~~ | **Feito (2026-07-06):** reforma da câmera do canvas atendendo o report do usuário ("pisca/muda de posição a cada ação; rola telas de preto à toa"). Princípio (padrão Figma/tldraw): **a câmera SÓ se move por comando explícito** — arrastar/roda, clique num job da sidebar (centraliza), botão Organizar, Force (foca o job forçado); criar/abrir/fechar job, trocar de aba, mudar folders, churn de dado: **nada re-enquadra** (única exceção: 1º paint de um modo sem câmera salva, enquadrado ANTES de pintar — sem pulo perceptível; o `setTimeout(140ms)+anim` da entrada morreu). Mudanças: (1) `viewContextKey` virou **só o modo** ("design"/"monitoring") — mudar folders não mexe na câmera; (2) `handleSaveDef` NÃO reorganiza mais ao criar job novo; (3) **limites de pan derivados do CONTEÚDO** (`contentBounds` em canvas-layout = fonte única com o Organizar), extent **dinâmico** (função de zoom+pane, folgas verticais em px de TELA): Monitoring atravessa até o último job sumir "por pouco" nos lados (1 tela + `PAN_CROSS_SLACK=40`), topo = `TOP_ANCHOR+32` px de tela, fundo = último job + 200px; Design preso à caixa dos jobs ±360 (vazio = câmera TRAVADA na origem); conteúdo mais baixo que a tela = **pin vertical** (zero rolagem de preto); (4) TODO movimento (pan/wheel custom + programático) passa pelo MESMO `clampCamera` via `apply()` (caminho único de escrita), e conteúdo que muda com a câmera fora do novo limite puxa de volta com ease 200ms; (5) `remember` contínuo por movimento (remount do RF por ViewPoint/CodeMode volta exato) e **câmera de canvas vazio nunca é salva** (pagehide na tela de login / visita a Design vazio não gravam posição por cima da real); (6) `zoomOnPinch`/`zoomOnDoubleClick` nativos desligados (caminho único); `translateExtent` vira só cinto de segurança (superset no pior zoom). Sensibilidade mantida (drag 0.6 / wheel 0.35). Validado AO VIVO com server GitOps em bare repo local (15 defs/3 folders): 4 limites de drag EXATOS na fórmula em z=0.5/1.1/2 · zoom clampa 0.5–2 e re-clampa posição · folder pequena no Design = imóvel sob drag de 50k px · criar job (drop+save), drawer abrir/fechar, Run Daily com folder nova (18→22 nós), troca de aba e F5 = câmera **imóvel/restaurada exata** · sidebar-click focou respeitando o limite inferior (y=-220 = clamp exato) · 1ª entrada do Design enquadrou com fit z=0.844/topo 88px. tsc + build verdes; eslint = paridade com baseline. |
| ✅ | ~~**Jobs as code (modo CODE Matrix) + FECHAMENTO do Aprofundamento Control-M (CTM-1/2/3)**~~ | **Feito (2026-07-06):** (1) **Jobs as code** — botão **CODE** estilizado Matrix na topbar do Design (verde fósforo `#00ff41`, varredura de "chuva" animada em CSS): o palco central vira um **editor YAML do working set** da design session (`CodeModeView`, com digital rain em canvas ~15fps ao fundo, gutter de linhas, highlight YAML por regex — chave/string/número/bool/comentário/`%%TOKEN` — via `<pre>` sob textarea transparente, zero dependência nova). Server: `GET/POST /api/design/sessions/{sid}/code` (`api/code.go`) — GET serializa as defs das folders abertas como **YAML multi-doc no MESMO dialeto dos arquivos do workspace Git** (ordenado por team/id, com comentário de path por doc); POST parseia **estrito** (campo desconhecido = erro, pega typo `retires:`), team default quando a folder é única, calcula o **plano creates/updates/deletes/unchanged** (igualdade por YAML canônico; job movido de folder = Save novo + Delete antigo), `apply=false` = dry-run (botão **Validar**), `apply=true` executa **transacional por item** (semântica do bulk) com ACL por folder, e **delete-por-ausência é gated** (`allowDelete` — o front pede confirmação listando os ids). Fluxo validado **AO VIVO E2E** (server single-origin + GitOps em bare repo local): editar → Validar (3 no doc, +1 criar) → Aplicar → job novo apareceu no canvas com a dependência. Fix de robustez: escopo de folders com chave estável no `CodeModeView` (array novo por render do pai refetchava e **apagava a edição**). Aperfeiçoamentos mapeados em **CODE-1**. (2) **CTM-1 `%%SETLOCAL NOME=VALOR`** — SET de variável com escopo **LOCAL por instance** (Control-M ctmvar local): persiste em `instances.local_vars` (JSON, migration **v10** SQLite+PG), aplicado a **cada término de tentativa ANTES do retry** (tentativa falha passa estado pra próxima — diferente do `%%SET` global, terminal-only), lido na interpolação da MESMA instance (novo escopo `Local` no `VarContext`, precedência Runtime > **Local** > Definition > Global), sobrevive a rerun e voltas cyclic, **nunca vaza** pro VariableStore nem pra outro job; evento de auditoria `set-var-local`; teto 20/término. (3) **CTM-2 tokens NATIVOS de data** — `%%EOM`/`%%BOM` (último/primeiro dia do mês), `%%EOY`/`%%BOY` (do ano), `%%NEXTBD`/`%%PREVBD` (próximo/anterior dia útil), `%%FIRSTBD`/`%%LASTBD` (1º/último dia útil do mês) como **VALOR direto** derivado do ODATE (formato compacto YYYYMMDD), cientes do **calendar do job** (`ctx.BusinessDay` — feriado desloca) e compostos com o offset existente (`%%EOM-1`, `%%LASTBD-1B`, `%%NEXTBD+2B`); nome definido pelo usuário tem precedência sobre o nativo; `${var.}` também resolve. (4) **CTM-3 Mass Update / Find & Update RICO** — `POST /api/design/sessions/{sid}/massupdate` (`api/massupdate.go`): **critério** (ids da seleção do canvas ∧ folders ∧ jobType ∧ **regex sobre campo** ∧ **campo vazio**) → **operação** (set-field c/ `onlyIfEmpty` · find-replace regex em campo arbitrário **ou em todos os strings de params** · add/remove-action · add/remove-upstream (self-loop no-op, condition default on-success) · set/remove-variable · add/remove-condition-in) → `apply=false` = **PREVIEW com diff por job** (field: before → after) → apply **transacional por item** (deep-copy p/ isolamento, validate+ACL por item) → **UNDO por session** (`/massupdate/undo`, pilha in-memory cap 10, morre com publish/discard/restart). Campo `schedule.description` adicionado ao modelo Go (o front sempre enviou e o server DROPAVA — bug de perda de dados corrigido de carona; casos "descrição vazia → preencher em lote" agora funcionam). UI: botão **FIND & UPDATE** na topbar do Design (`MassUpdateDialog`: critério → operação com campos contextuais → preview tabelado → aplicar → botão ↩ Desfazer). Validado **AO VIVO E2E**: preview 4 jobs (∅→valor), apply gravou, undo restaurou ∅; regex `-fin$` + find-replace em params conferidos via API. **17 testes Go novos** (scheduler: 5 CTM-1/2 · api: 12 code+massupdate) + suítes server/api/db + `scripts/verify.sh` verdes. |
| ✅ | ~~**BUG 👻GHOST: selo do Monitoring mudava em tempo real ao publicar no Design** (imutabilidade)~~ | **Feito (2026-07-04):** o selo 👻GHOST acendia/apagava em jobs **já ordenados** ao ligar `dryRun` no Design e publicar — violação da regra de que o **Monitoring é IMUTÁVEL** (uma instância só muda numa NOVA ordem: daily/force/rerun). Raiz: era o ÚNICO atributo do card derivado da **definition VIVA** (`dryRun: !!defsById.get(inst.definitionId)?.dryRun` em `buildMonitoringCanvas`), enquanto `team` e o resto já vinham congelados na instância. Fix (mesmo padrão do `team`, schemaV4): **coluna `dry_run` snapshotada na ordem** (`schemaV9`, SQLite+PG), congelada nos DOIS INSERTs — daily batch (`insertDailyChunk`) e `ForceOrder` — a partir de `def.DryRun`; exposta na API (`instances.go`: instanceRow/instanceCols/scanInstances); carregada no `toWeb` (server-instance-store.ts); e o canvas passou a ler `!!inst.dryRun` (snapshot), NUNCA a def viva. Path local/demo já congelava certo via `createInstance`. Comentários em cada ponto explicando a invariante. Teste `TestDryRun_FrozenOnOrder_NotRewrittenByLiveDef` (mexer na def viva NÃO reescreve a instância; a próxima ordem/force sim congela o novo valor) + `go build`/`vet`/suítes scheduler·api·db + `tsc` verdes. |
| ✅ | ~~**Selo 👻GHOST no card de job dry-run** (Refinamento UI)~~ | **Feito (2026-07-04):** job com `dryRun:true` (entra na daily e "roda", mas NÃO executa — log only) agora tem selo próprio no card do Monitoring, análogo ao ⚡FORCED do Force Order. `JobNodeV2` renderiza `👻GHOST` (lavanda `#c4b5fd`) ao lado do label quando `data.dryRun`; o flag vem do **snapshot da instância** (`inst.dryRun`, coluna `dry_run` congelada na ordem — ver a linha do bug de imutabilidade acima; a versão original lia a def viva e foi corrigida). Só monitoring (como o FORCED). tsc limpo + **validado AO VIVO** (server offline demo-mode, workspace c/ 1 job normal + 1 dryRun: card PIPELINE-GHOST com 👻GHOST, PIPELINE-REAL sem; cor computada `rgb(196,181,253)` confirmada no DevTools). |
| ✅ | ~~**Cadeado no card de job em HOLD** (Refinamento UI)~~ | **Feito (2026-07-04):** job segurado manualmente por um operador (Control-M "Hold") agora é sinalizado no card do Monitoring com um **cadeado sobreposto no canto superior esquerdo**, fundo transparente, sobre a barra de status. Motivação: `HOLD` colapsa para `INACTIVE` na cor de status (igual a `CANCELLED`), então "segurado" e "ocioso/cancelado" ficavam visualmente idênticos — o cadeado é o que os distingue. `JobNodeV2` renderiza o `Lock` (lucide, lavanda `#c4b5fd`, `size 11`) em `position:absolute` (top-left, `pointerEvents:none`, tooltip explicativo); a Linha 1 ganha `paddingLeft` reservado quando `held` pra o cadeado não cobrir as primeiras letras do label. O flag vem do **snapshot da instância** (`held: inst.status === "HOLD"` em `buildMonitoringCanvas`, mesmo padrão do `dryRun`/`forced`); server manda `HELD`, o store mapeia → `HOLD`. Só monitoring. tsc limpo (2×) + **validado AO VIVO** (server offline :9090, def COMMAND → Force → `POST /instances/{id}/hold`: card com cadeado em x6/y4, label começa em x27 = **sem overlap**, tooltip e cor confirmados no DevTools; convive com o ⚡FORCED no mesmo card). |
| ✅ | ~~**Dashboards prontos** (presets do /summary na UI) + **Cross-nó** (frota multi-node R5)~~ | **Feito (2026-07-03):** dois polimentos de apresentação. (1) **Dashboards prontos** — barra de presets clicáveis no ViewPoint (`ScaleMonitor`): "Visão geral" + um por estado (Rodando/Aguardando/Concluídos/**Falhas**/Em espera). **Zero backend novo** — reusa `GET /instances/summary?status=` (que já filtrava): o preset ativo reescopa o total, a lista de folders (só as que têm jobs naquele estado) e a página de jobs da folder aberta, enquanto os cards de estado continuam mostrando o dia inteiro (o "dashboard"). 2º `/summary` filtrado só quando há preset; reset volta ao global. Validado ao vivo (5k jobs seed, server:9090): "Falhas" → **625 de 5.000 em 3 folders** (APP_002/006/010, batendo o backend); "Visão geral" → 12 folders/5.000. (2) **Cross-nó (R5)** — a tela de Agentes reflete o **CLUSTER**, não só este nó: `bus.Distributed.RemoteAgents()` expõe a presença R5 (agents vistos em outros nós, com TTL) e `GET /api/agents` faz a UNIÃO hub-local ∪ presença-remota → agent conectado no node-2 aparece **online** (campo `node`) em vez de fantasma offline; `local` marca quem é pingável daqui (só o do próprio nó → botão "ping" só neles); agent remoto sem linha no DB deste nó ainda entra na frota. `Config.Presence`/`NodeID` (nil = single-node idêntico ao anterior). UI: chip "⇄ node" + linha "Nó" no detalhe. `TestAgents_CrossNodePresence` (presença mockada, sem NATS) + build/tsc/go test verdes. |
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
| ✅ | ~~Câmera lembra a posição por view + centraliza na faixa visível~~ | **Feito (2026-07-03, commit `1d3230a`):** dois incômodos ao alternar Monitoring↔Design. (1) A cada troca de aba a câmera **re-organizava** ("desalinha e realinha" + perdia a posição — centralizou o Monitoring, voltava descentralizado). Causa: o effect de auto-organizar disparava a cada mudança de `viewContextKey` (modo/folders). Fix: **persistência por view** (`savedViewports` Map em `useCanvasCamera`) — ao SAIR guarda o viewport (só quando o contexto muda de fato, via `prevKeyRef`, não no churn de load que gravaria o 0,0 inicial); ao ENTRAR restaura sem animar (`useLayoutEffect` → sem flash) ou enquadra só na 1ª vez. `hasNodes` é boolean (0↔N) → churn de dado não re-dispara. (2) Design nascia "**pra esquerda**": `organizeView`/`focusOnPoint` centralizavam na largura TOTAL do pane do RF (ocupa o palco inteiro), mas sidebar (esq.) e drawer (dir.) são overlays POR CIMA. Fix: `visibleInsets()` mede os overlays (marcados com `data-canvas-inset` em Monitoring/DesignSidebarV2 + InstanceDetailsDrawer) e centraliza/enquadra na faixa `[sidebar..drawer]`. Verificado ao vivo (board organiza; com drawer aberto centraliza no espaço restante). tsc limpo. |
| ✅ | ~~Cards somem em rajada — CAMADA 3 (raiz real: RF `visibility:hidden` preso)~~ | **Feito (2026-07-03, commit `8d86bf1`):** o sintoma sobreviveu às camadas 1/2 (delete-by-absence + 200 truncado, `0cc167e`/`fc78f63`) porque essas blindaram o **DADO**, não a **RENDERIZAÇÃO**. Diagnóstico ao vivo (Chrome DevTools no lab do `.bat`): sob rajada de rerun/cancel/force o board "some" e só volta com F5, mas o dado está intacto (server tem os 7, store não esvazia same-date, **sem crash/ErrorBoundary** — sidebar e drawer seguem renderizando). Prova irrefutável: os `.react-flow__node` existiam no DOM com **`visibility:hidden` e `measured:false`**; forçar `visibility:visible` trazia o board INTEIRO de volta na hora. Raiz: o **ReactFlow v12** renderiza cada nó com `visibility: hasDimensions ? 'visible' : 'hidden'` e só marca visível **após seu ResizeObserver MEDIR** as dimensões; sob rajada o observer estoura (`ResizeObserver loop completed with undelivered notifications` — capturado) e a medida nunca chega → nós presos em hidden → board vazio; F5 remonta e re-mede limpo. Fix: dar dimensão de cara aos nós em `canvas-layout.ts` via **`initialWidth`/`initialHeight`** (lane = colWidth×colHeight; job = NODE_W×NODE_H). `nodeHasDimensions` usa `measured ?? width ?? initialWidth` → hasDimensions=true já na 1ª render, visibilidade NUNCA depende do ResizeObserver. Verificado ao vivo pós-fix (reload HMR): os 8 nós nascem `visibility:visible` (hidden:0), board completo com edges. tsc limpo. |
| ✅ | ~~Linha de dependência some em rajada — CAMADA 4 (mesma raiz da 3, agora na ARESTA)~~ | **Feito (2026-07-04):** depois da camada 3 (nós blindados com `initialWidth`), o usuário reportou o MESMO sumiço agora nas **linhas de dependência**. Raiz simétrica no **ReactFlow v12**: `getEdgePosition()` exige `isNodeInitialized` = dimensão **E** `internals.handleBounds` (posição dos handles). A camada 3 satisfez a dimensão (`initialWidth`), mas o `handleBounds` só é populado quando o **ResizeObserver MEDE** o nó — e pior: a cada rebuild do array de nós (todo update de status) o `adoptUserNodes`→`parseHandles` **zera** `handleBounds` pra `undefined`, porque ele só preserva a medição anterior quando `userNode.measured` existe (e nossos nós não carregam `measured`). Com `handleBounds` nulo, `getEdgePosition` retorna `null` → **a edge não renderiza**; sob rajada o re-measure estoura e a linha fica sumida até um F5. Fix: declarar **`handles` estáticos** (`JOB_HANDLES` em `canvas-layout.ts`) nos dois builders de job (monitoring+design) — aí o fallback `toHandleBounds(node.handles)` mantém a aresta sempre posicionável, e a medição real do DOM assume depois (`getEdgePosition` prefere `internals.handleBounds`). Geometria = padrão do RF pro card 200×55 com nub de 6px (centro horizontal, 3px além de cada ponta), medida no lab → fallback coincide com o measured, sem "pulo". Provado contra o `@xyflow/system` real: sem handles `getEdgePosition`→NULL (some); com handles→`src(100,58)`/`tgt(100,197)` (posições exatas). Cobertura: as edges do canvas só saem de `buildMonitoringCanvas`/`buildDesignCanvas` (V2Preview); ScaleMonitor não desenha aresta e FolderCardsView expressa dependência como texto fora do ReactFlow. tsc limpo. |
| ✅ | ~~CI vermelho: flake `TempDir RemoveAll ... directory not empty` no pacote `scheduler`~~ | **Feito (2026-07-04):** o job `server` do CI falhava (não-determinístico) em `TestTick_BlockedSuccessorWaitsAndRunsAfterSetOK` e `TestAlertEngine_NoWebhookConfigured` com `testing.go: TempDir RemoveAll cleanup: unlinkat /tmp/.../001: directory not empty` — recorrência do "tempdir flake" (27ad8b9). Raiz: **goroutines fire-and-forget escreviam no DB do `t.TempDir()` DEPOIS do teste retornar**, correndo com o `RemoveAll` do cleanup. Duas fontes: (1) o dispatch em **DemoMode** (`go func` no `startInstance`) fazia `time.Sleep(1s)`→`FinishInstance` (write) sem ser rastreado/cancelável, e o backoff de **retry** (`time.AfterFunc(5s)`→`startInstance`) idem; (2) o **AlertEngine** fazia `go e.route(...)` (lê settings do DB) mesmo sem nenhum sink externo configurado — no-op que só existia pra vazar. Fix: (a) ciclo de vida no `Scheduler` (`quit`+`sync.WaitGroup`+`Stop()`; `sleepOrStop` torna os sleeps de fundo abortáveis; dispatch e retry rastreados no wg) e `t.Cleanup(s.Stop)` no helper de teste (LIFO: roda ANTES do `db.Close`) → nenhum write sobrevive ao teardown; (b) `hasExternalSink()` gateia os 3 `go e.route` (`fire`/`FireSystem`/`FireAction`) — sem canal (o default), não spawna goroutine (mata o flake e ainda poupa trabalho em produção). Verificado: `go build`/`go vet ./...` limpos, suíte completa verde, `scheduler -count=5` (estresse) verde. Os testes de webhook/pagerduty (que configuram sink) seguem passando; o pacote `api` não dispara em teste, sem risco análogo. |
| ✅ | ~~Refinamento UI: drafts do Design à prova de F5 (retomada de sessão)~~ | **Feito (2026-07-02, commit `317ecd0`):** bug do usuário — abrir folder → Monitoring/F5 → Design voltava VAZIO; reabrir folder criava OUTRA session por cima da esquecida (trabalho invisível, "folders bugam"). Causa: o `designSessionId` vivia só em memória de módulo — o clone com o trabalho seguia vivo no server (P6: DB+disco), mas a UI perdia o PONTEIRO sem caminho de recuperação. Fix em 3 camadas: (1) **ponteiro persistido** (`localStorage regente:designSessionId` + restore no boot com claim P7; 404 limpa, erro transiente NÃO derruba); (2) **recuperação/auto-descarte** (effect pós-auth lista sessions do actor: valida posse do sid, auto-descarta LIMPAS idle >10min, SUJAS viram banner Retomar/Descartar no canvas; addFolder sem session RETOMA a suja mais recente em vez de criar por cima); (3) **server protege trabalho** (`DesignSession.Dirty()`; GC por TTL PULA sessions sujas — antes 24h idle apagava trabalho; `sweepCleanIdle(actor,10min)` no Create; `dirty` exposto no list/get via `sessionView`). `session_test.go` (TestDesignSessionDirtyProtection). Validado ao vivo: F5 retoma com o job no canvas; banner recupera órfã; abrir folder-2 por cima reusa a MESMA session (escopo f1+f2, dirty intacto); limpa idle auto-descartada. go test ./... + tsc + build ok. |
| ✅ | ~~**Aprofundamento Control-M (restante)** — cyclic · CONFIRM · DATABASE · SET var + datas · baterias · ViewPoints~~ | **Feito (2026-07-03):** TRILHA 100%. Ver a linha do topo do changelog. |
| ✅ | ~~**Diferenciais (leva 1)** — Job Neighborhood · RCA · Event log · NL-query~~ | **Feito (2026-07-03).** Ver a linha do topo do changelog. |
| ✅ | ~~**Diferenciais (leva 2, D-1..D-15)** — orquestração híbrida/stateful · DevEx (schedule-as-code · `regente test`/`dev`/`promote`) · promotion · policy-as-code · chaos · "wow" (Gantt · templates · portal · quick-actions)~~ | **Feito (2026-07-08).** FECHA a trilha Diferenciais a 100%. Ver a linha do topo do changelog. |
| ✅ | ~~**Refinamento UI** — grade de jobs soltos · minimap revisto · virtualizar ACTIVE JOBS (LEGACY_CAP)~~ | **Feito (2026-07-09).** Grade+minimap já estavam; UI-1 (sidebar virtualizada @1M) + UI-3 (grade por folder) fecham. Ver a linha do topo do changelog. |
| 🏁 | **Fase Z** — case study + post LinkedIn | **ÚLTIMO gate, por definição.** Só quando o backlog acima estiver onde você quer. NÃO é o próximo passo. |

---

## 🏢 Backlog Enterprise E1..E6 *(SPEC/histórico — trilha ENTREGUE em 2026-07-07)*

> ⚠️ **Já entregue** (ver o §📜 Changelog: "E1+E2+E3" e "E4+E5+E6"). Mantido aqui como a **spec
> de implementação** que guiou o trabalho — vira doc de referência da feature. NÃO é backlog aberto.

Itens da avaliação enterprise, especificados para implementação SEM ambiguidade.
Regras gerais para TODOS os itens: (a) mudanças de schema = nova migration versionada
em `internal/db` (SQLite E Postgres); (b) settings novos via tabela `settings`
(editáveis por `PUT /api/settings`, admin-only) com fallback no default do processo;
(c) todo item entrega testes Go + validação ao vivo documentada no commit; (d) nada
quebra o modo demo/single-node — features avançadas são opt-in com default seguro.

### E1 — Timezone da daily (settings.daily_timezone) ✅ *(entregue 2026-07-07 — ver changelog)*
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

### E2 — Auditoria enterprise (retenção + export + audit de settings) ✅ *(entregue 2026-07-07 — ver changelog)*
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

### E3 — RBAC de escrita por AÇÃO OPERACIONAL (folder-scoped) ✅ *(entregue 2026-07-07 — ver changelog)*
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

### E4 — Fila assíncrona de eventos de instance (protege o hot path) ✅ *(entregue 2026-07-07 — ver changelog)*
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

### E5 — Relatório/SLO da daily (o artefato que operação cobra) ✅ *(entregue 2026-07-07 — ver changelog)*
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

### E6 — Importador Control-M (redutor de fricção nº 1 p/ adoção) ✅ *(entregue 2026-07-07 — ver changelog + README do cmd)*
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

## 🧩 Aprofundamento Control-M *(SPEC/histórico — status resumido no §✅ Entregue)*

> ⚠️ **As marcações abaixo foram corrigidas em 2026-07-08 (auditoria).** A trilha está **100%
> FECHADA** pro que foi CONSTRUÍDO (núcleo 2026-07-03; CTM-1/CTM-2/CTM-3 em 2026-07-06 —
> `%%SETLOCAL` · tokens nativos de data · Mass Update rico, ver changelog) — os itens abaixo que
> eram duplicatas de features já ✅ no §Entregue foram atualizados pra ✅ com ponteiro. As baterias
> de teste exaustivas (**CTM-4/CTM-5/CTM-6**) foram FECHADAS em 2026-07-08 (ver changelog no topo) —
> incluindo o fix da divergência do `Forecast()` (só olhava o calendar legado) e o novo
> `ForecastRange`/`GET /api/forecast/range` para o "≥1 semana à frente".

```
✅ Calendários complexos → CTM-4 FECHADO (2026-07-08, `ctm_batteries_test.go`); base (`IsScheduledOn`,
   `schedule.shift` next/prev-businessday, advanced/nth-businessday) já entregue.
✅ Controle de recursos → CTM-5 FECHADO (2026-07-08, `ctm_batteries_test.go`); feature (quotas, F15,
   RebuildResourcesFromRunning) já entregue.
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
✅ Forecast → CTM-6 FECHADO (2026-07-08, `ctm_batteries_test.go`): Forecast passou a usar `IsScheduledOn`
   (fim da divergência com a daily) + `ForecastRange`/`GET /api/forecast/range` (≥1 semana), validado
   contra oráculo escrito à mão dia-a-dia; feature (D-4 p50/p90/tendência/ETA) já entregue.
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
✅ CONFIRM → já entregue (gate WAIT_CONFIRM, nem Force bypassa), ver §Entregue L351.
✅ Job tipo DATABASE → já entregue (SQL Postgres/MySQL/SQLite pelo agente, drivers pure-Go), ver §Entregue L352.
✅ ViewPoint (Monitoring) → já entregue (ViewPoints salvos/upsert-por-nome/shared), ver §Entregue L355.
✅ Dashboards prontos → já entregue (barra de presets clicáveis reusando /summary?status=), ver §Entregue L355
   e changelog 2026-07-03 commit 719bd1b.
✅ Mass Update / Find & Update (Design) → já entregue CTM-3 (critério ids/folder/jobType/regex/campo-vazio →
   operação → preview diff → apply → undo pilha cap 10), ver §Entregue L360.
✅ Sistema de variáveis (estilo Control-M %%) → COMPLETO. Interpolação (2026-07-02) + SET em runtime
   (`%%SET NOME=VALOR` no output → VariableStore global) + escopo LOCAL por instance (CTM-1 `%%SETLOCAL`,
   aplicado antes do retry) + tokens nativos extras (CTM-2: EOM/BOM/EOY/BOY/NEXTBD/PREVBD/FIRSTBD/LASTBD,
   cientes de calendar, componíveis com offset `%%EOM-1B`) + cálculo de datas (`%%ODATE±N/±NB`, dias úteis
   via `ctx.BusinessDay`). Ver §Entregue L353/356/358-359.
```

## ⬜ Features avançadas *(depois do núcleo sólido — SPEC; status no §🔜 Backlog como ADV-1..ADV-8)*

- Job types com schema dedicado · Multi-ambiente/multi-site · What-If/Forecast/Statistics
- MFT (FILE_TRANSFER nativo) · Archives/Retention · Import de Control-M · CLI/SDK · site de docs
- **Executores AWS extras** (Batch/Glue/Step) — adapters por capability (Lambda já feito); validação em conta
  paga fora de escopo por decisão

## 🌟 Diferenciais — visão de produto *(o racional)*

> **Onde o Regente passa o Control-M**, organizado por tema — o *porquê* de cada diferencial.
> O **status e a implementação** de cada um estão no §✅ Entregue (seção "🌟 Diferenciais além do
> Control-M", trilha fechada). Esta seção é a narrativa pra Fase Z / case study — não repete status.

1. **Orquestração híbrida e stateful** *(grande gap do Control-M)* — human-in-the-loop + long-running
   (workflows de dias/semanas: "aprovação manual + retry após 3 dias"), pausa/resume com **estado** preservado
   (além do Hold) e event-driven **confiável** (reage a evento externo sem polling). → D-1, D-2, D-3.
2. **Observabilidade e análise avançada** — Explain ("por que não rodou?", sem IA) · Diff de Daily (barato
   via Git-native) · Blast Radius (impacto de uma ação) · Dry Run (simular sem materializar) · Job Neighborhood ·
   RCA · Event log CQRS-lite · forecasting com gráficos · query estruturada/busca rica. → Leva 1 + D-4, D-5.
3. **Developer experience** *(onde o Control-M perde feio)* — schedule-as-code (YAML + DSL), `regente test`
   (simula com o engine real), `regente dev daily` (daily local descartável). → D-6, D-7, D-8.
4. **Enterprise & operação** — multi-environment promotion (Dev→Staging→Prod, Git-native), policy-as-code
   (regras obrigatórias: SLA/retry/owner…), chaos ("Inject Failure"). → D-9, D-10, D-11.
5. **Features "wow"** — Visual Schedule Editor (Gantt da daily), templates reutilizáveis, self-service portal
   (negócio roda jobs aprovados sem tocar no Design), mobile alerts com ações rápidas. → D-12, D-13, D-14, D-15.

> Decisão de arquitetura preservada (Event log): evoluir `instance_events` p/ um log CQRS-lite
> (emissão transacional, sequência global) destrava replay/time-travel/forense — **ES puro foi
> avaliado e rejeitado** por ROI/risco (HA/DR/auditoria/histórico-de-config já cobertos por
> Postgres+leader · PITR · instance_events · Git). O que existe hoje (`GET /events`) é a projeção read-model.

---

## 🏁 Fase Z — ÚLTIMO GATE

Case study técnico + post LinkedIn — **só com tudo sólido** e o Backlog onde você quiser. Um
orquestrador que cai e não volta sozinho não vira post; a trilha Resiliência (R1–R7) já fecha isso.
