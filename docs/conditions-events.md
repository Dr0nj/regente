# Condições — especificação de comportamento (FONTE ÚNICA)

> **LEIA ESTE ARQUIVO INTEIRO antes de tocar em qualquer código de dependência,
> condição, rerun, Set OK ou Force.** Ele é a fonte única da semântica; o
> roadmap registra *quando* cada regra entrou, mas o comportamento canônico é o
> daqui. Se um patch contradiz este doc, ou o patch está errado ou este doc
> precisa mudar JUNTO (no mesmo commit, com o porquê).

## O modelo único (2026-07-17)

**Toda dependência é uma CONDIÇÃO nomeada num POOL global** (Control-M global
conditions). Não existem mais dois sistemas ("setas do grafo" × "conditions
F16") — o report do usuário que unificou: *"todas as condições de dependências
têm que ser uma coisa só em termos de arquitetura; tanto ligando pela caixa
quanto colocando na mão, as condições vão para o mesmo lugar"*.

O pool é a tabela `conditions` (`name`, `scope_date`, `set_at`, `set_by`):

- `scope_date = 'YYYY-MM-DD'` → condição daquela diária (ODAT);
- `scope_date = ''` → permanente/estática (`@stat`).

Cada job declara três listas (drawer Design → aba **Condições**; YAML;
mass-update; MCP):

| campo | papel | quando age |
|---|---|---|
| `conditionsIn` | **entrada** — depende de | gate: o job só roda quando TODAS existem no pool, no escopo resolvido do sufixo contra o SEU ODAT |
| `conditionsOutAdd` | **saída＋** — adiciona | no término **OK ou Set OK** |
| `conditionsOutRemove` | **saída−** — deleta (CONSUMO) | no término **OK ou Set OK** |

**A setinha do Design é açúcar de UI.** Ligar A→B cria a condição
`LinkCondName(A,B) = "A-TO-B"` (uppercase dos ids): **saída＋ em A**;
**entrada E saída− em B** (a deleção é automática na ligação — regra do
usuário). Digitar à mão os MESMOS nomes dá exatamente no mesmo lugar; quem
liga à mão precisa colocar o nome **na entrada E na saída−** se quiser
semântica de consumo (sem a saída−, a condição sobrevive ao OK — fan-out N:M
deliberado).

Ergonomia da setinha (2026-07-17, report "arrasto e não cria a dependência"):
a bolinha tem pegada de 18px (nub visual segue 6px), o drop tem ímã
(`connectionRadius`) e **soltar sobre o CARD do outro job liga** (fallback
`onConnectEnd` — não precisa acertar o handle). A DIREÇÃO segue a bolinha de
origem: a de **baixo** = este job produz pro alvo; a de **cima** = invertida —
o job arrastado passa a DEPENDER do alvo. Os campos escritos são os mesmos nos
dois gestos (invariante 9). O drawer do Design faz **AUTOSAVE** (trocar de
job/fechar salva o que estava sujo; job novo segue exigindo Save) e recebe a
def VIVA, mesclando por diff condições escritas por fora — um drawer aberto
nunca mais sobrescreve a ligação recém-criada pela setinha.

**Quem cria/remove condição no pool:**
1. término **OK/Set OK** de um job (saída＋/saída−) — `ApplyOutcomes`, no ODAT
   do produtor;
2. ação **On/Do `set-condition`** (ex.: no NOTOK — é como arestas legadas
   `on-failure` são expressas);
3. **operador**, pelo painel **Condições** do Monitoring (botão ao lado do
   Organizar — list/add/delete com data) ou API/MCP;
4. evento externo (`POST /api/events/ingest`).

Toda mudança no pool emite WS **`condition.changed`** (o painel e as linhas do
grafo são reflexo ao vivo) e cutuca o tick (quem esperava roda NA HORA).

## Datas — ODAT / PREV / STAT

**ODAT** = data de ORIGEM da ordem (Control-M ODATE):
`ODAT = COALESCE(carried_from, order_date)` (`scheduler/odate.go`). O
carry-over avança `order_date` preservando a origem — **todo escopo de data
usa ODAT**, nunca o dia ativo avançado (job carregado do dia 14 cria e procura
condições DO DIA 14).

A data de uma condição vive como **sufixo no nome**, editado pela caixinha de
seleção por linha (o usuário nunca digita arroba):

| seletor | sufixo | significado (relativo ao ODAT do job) |
|---|---|---|
| **Odate** (default) | sem sufixo / `@odat` | diária de origem (entrada procura `scope=ODAT` ou permanente; saída cria no ODAT) |
| **Prev** | `@prev` | diária ANTERIOR = último New Day em `daily_runs` antes do ODAT; sem registro, ODAT−1 |
| **Stat** | `@stat` | permanente: entrada só enxerga `scope_date=''`; saída cria/remove a permanente |

## As regras (numeradas — cite pelo número em commits/testes)

**C1 — Gate.** Job WAITING roda quando TODAS as `conditionsIn` existem no pool
(escopo resolvido). Condição ausente = **WAIT COND** (card, Explain
`WAIT_CONDITION`) — espera indefinida, NUNCA auto-cancel (paridade Control-M;
quem nunca ficar elegível morre na virada da daily se keepActive permitir).
Hot path: o tick carrega o pool UMA vez por ciclo (`CondIndex`).

**C2 — OK aplica as saídas.** Término OK (real) e **Set OK** (BUG-3/4)
aplicam saída＋ e saída− — nos DOIS papéis: como produtor destrava sucessores;
como consumidor **CONSOME** a própria entrada (a saída− armada pela setinha).
NOTOK terminal NÃO aplica nada.

**C3 — Consumo = deleção no OK.** A "permanência do consumo" do modelo antigo
agora é consequência natural: rerun de um consumidor que terminou OK entra em
WAIT COND, porque o próprio OK apagou a condição de entrada. **Set OK + rerun
= aguardando** (o Set OK foi no pool e deletou — cenário-guia do usuário).
Rerun após **NOTOK/CANCELLED** roda direto (falha não consome; a condição
segue no pool).

**C4 — Rerun/cancel/delete/hold NÃO tocam o pool.** Nenhuma ação de operador
sobre instances mexe em condição; só términos OK/Set OK (C2), ações On/Do e o
operador PELO PAINEL. Rerun do PAI cria condição NOVA no próximo OK — é o que
destrava o rerun do filho.

**C5 — Force.** `Order Force` (Design) cria ordem nova que bypassa SÓ o
agendamento — o gate C1 vale (cópia de consumidor já consumido nasce em WAIT
COND até alguém recriar a condição). Se a condição AINDA existe no pool, a
cópia roda — pool puro, sem trava por-instância (mudança deliberada vs. o
modelo de claims). `Run Now` (Monitoring) bypassa C1 por completo; o bypass
NÃO é pegajoso (rerun zera `forced` quando `force_mode=''`).

**C6 — Sem condição imutável.** O operador pode deletar/adicionar QUALQUER
condição no painel; o efeito é imediato (deletou → dependente volta a esperar;
adicionou → dependente roda na hora). Regra do usuário: *"não teremos nenhum
tipo de condição imutável"*.

**C7 — JSON nunca null.** Toda lista da API (`blockers` do Explain, etc.)
serializa `[]`, nunca `null` (slice nil derruba o front).

## `upstream[]` — legado e visão derivada

O campo `upstream` NÃO é mais gate nem é persistido:

1. **Compat de leitura**: YAML antigo com `upstream:` é EXPANDIDO em condições
   explícitas no chokepoint de leitura (`FileStore.List` →
   `domain.NormalizeConditions`, idempotente — cobre scheduler, API, sessions
   e publish; espelho TS em `lib/conditions-model.ts` p/ modo local):
   - `on-success`/vazio → saída＋ no pai; entrada + saída− no filho;
   - `on-complete` → idem + ação On/Do `set-condition` no NOTOK do pai;
   - `on-failure` → SÓ a ação On/Do no NOTOK (nada no OK);
   - `always` → sem gate (vira só topologia);
   - `dateRef` vira o sufixo da entrada/saída− do filho; aresta `@stat` faz o
     pai criar a PERMANENTE (único escopo que um IN `@stat` enxerga).
2. **Visão derivada**: depois da expansão, `upstream` é RECALCULADO por
   name-matching produtor→consumidor (base do nome, ignorando sufixo) e serve
   só de topologia para canvas/WhatIf/RCA/forecast/vizinhança. `FileStore.Save`
   SEMPRE o descarta (persistir a visão re-expandiria como aresta legada).

**Snapshots** ordenados antes da unificação: `defForInstance` expande o lado
consumidor em memória (`ExpandSnapshotConditions`, com a regra de idempotência
contra as defs vivas); `applyConditionsOut` usa a UNIÃO snapshot∪def-viva
(o OutAdd sintetizado do pai legado vive na def viva). Backfill one-time no
boot (`MigrateConditionsUnify`, flag em `meta_flags` schemaV17): par
(pai já OK, filho WAITING não-consumido) semeia a condição no pool.
**dep_events/dep_claims (schemaV15) estão APOSENTADAS** — tabelas ficam no
banco por compat, nada lê/escreve.

## Linhas do Monitoring — reflexo do pool

Topologia = visão derivada (`def.upstream`); pares por cópia como antes
(`parentsForEdge` filtra a diária que o dateRef aceita — carregado do 14 só
ganha linha com o pai do 14). A COR é o estado das condições que LIGAM o par
(`edgeCondNames`: entradas do filho que o pai produz) para ESTE consumidor:

- **verde ✓** — a condição existe no pool, OU o consumidor já rodou sobre ela
  (RUNNING/OK — no OK ele consome, mas a linha segue verde: deu certo);
- **vermelho ✗** — o job SUBSEQUENTE está NOTOK;
- **cinza** — a condição não existe (ainda não criada · consumida por um OK —
  ex.: Set OK + rerun · deletada no painel) e o consumidor espera.

O CARD (**WAIT COND**) usa a régua do gate (C1): WAITING com QUALQUER entrada
ausente (todas as `conditionsIn`, não só as de setinha); `Run Now` (manual)
não acusa. Fonte no front: `isWaitingOnConds` + pool do `conditions-store`
(WS `condition.changed` ressincroniza; modo local usa localStorage com a MESMA
semântica, incluindo `applyOutcomesLocal` no OK).

## Ciclo de vida da daily (carry-over) — quem atravessa a virada

(Inalterado pela unificação.) Regra pura em `carryDecision` (scheduler.go);
idades em **DIAS-CALENDÁRIO** (timezone da daily):

| estado na virada | atravessa? |
|---|---|
| RUNNING | SEMPRE |
| HELD | SEMPRE, enquanto em hold |
| NOTOK | `dias desde a FALHA ≤ keepActive\|1` |
| WAITING com retry AGENDADO (`attempts>1` **e** `started_at`) | regra do NOTOK |
| WAITING (incl. CONFIRM) | `dias desde o ODAT ≤ keepActive` — keepActive=0 morre na 1ª virada |
| OK / CANCELLED | nunca |

Hold GERAL + Delete: hold vale pra qualquer status exceto RUNNING
(`held_from_status` restaurado no release); Delete só em HOLD. Nenhum dos dois
toca o pool (C4).

## Invariantes (o que NUNCA pode voltar a acontecer)

1. Rerun de consumidor que terminou OK re-rodando **sem condição nova** (C3).
2. Set OK deixando de aplicar saída＋ ou saída− (C2).
3. Deleção no painel NÃO travando o dependente, ou adição NÃO liberando (C6).
4. Bypass de Run Now sobrevivendo a um rerun (C5).
5. API mandando `null` onde o front espera lista (C7).
6. Monitoring derivando estado de card da def viva (é snapshot; exceções
   deliberadas: `waitConfirm`/`waitAgent` — o WAIT COND lê o POOL, que é
   estado de runtime, não def).
7. Condição de uma ORIGEM satisfazendo consumidor de OUTRA origem sem o
   sufixo pedir (§Datas — job do dia 14 não come condição de hoje).
8. `upstream` derivado sendo PERSISTIDO (re-expandiria como aresta legada).
9. Ligação pela setinha e ligação à mão divergirem em QUALQUER coisa — os dois
   caminhos escrevem os mesmos campos.

## Onde está no código

- `server/internal/domain/conditions.go` — modelo único: `SplitCondRef`,
  `LinkCondName`, `NormalizeConditions` (expansão idempotente + visão
  derivada), `ExpandSnapshotConditions`.
- `server/internal/scheduler/conditions.go` — pool (`ConditionEngine`):
  Set/Unset (broadcast via OnChange), `CondIndex` (foto por tick),
  `MissingIdx` (fonte única do "falta qual?"), `ApplyOutcomes`.
- `server/internal/scheduler/scheduler.go` — gate no tick (condIdx),
  `FinishInstance`/`SetOK` → `applyConditionsOut` (união snapshot∪viva),
  `defForInstance` (expansão de snapshot), `ForceOrder`.
- `server/internal/scheduler/condmigrate.go` — backfill one-time (meta_flags).
- `server/internal/scheduler/explain.go` — `gateInstance` (WAIT_CONDITION),
  Explain.
- `server/internal/storage/file.go` — List normaliza; Save descarta upstream.
- `server/internal/api/bloco2.go` + router — GET/POST `/api/conditions*`.
- `app/src/lib/conditions-model.ts` — espelho TS puro (normalização, pool
  helpers, `edgeCondNames`, `missingConds`).
- `app/src/lib/conditions-store.ts` — pool no front (server WS / localStorage;
  `applyOutcomesLocal`).
- `app/src/v2/ConditionsPanel.tsx` — painel Condições (Monitoring, ao lado do
  Organizar).
- `app/src/v2/canvas-layout.ts` — linhas pool-aware (`evaluateEdgeState`),
  `isWaitingOnConds`, espaçamento (NODE_GAP_Y=72) e ✓ 9px.
- `app/src/v2/V2Preview.tsx` — `connectDefs` (setinha → condições nos dois
  jobs; `onConnect` + fallback `onConnectEnd` de drop-no-card, direção pela
  bolinha de origem), pool state, botão Condições.
- `app/src/v2/JobConfigDrawer.tsx` — aba **Condições** unificada; autosave do
  draft (troca de job/fechar) + merge por diff das condições externas.

## Testes que travam a semântica

`scheduler/conditions_unify_test.go` (normalização/idempotência/visão ·
fluxo feliz com consumo · rerun após OK espera e após NOTOK roda · Set OK nos
dois papéis (C2) e Set OK+rerun aguardando (C3) · deleção/adição pelo painel
(C6) · Order Force segue o pool (C5) · on-failure vira ação · backfill da
migração) · `scheduler/lifecycle_test.go` (carryDecision; `TestOdat_*`
reescritos pro pool: escopo de origem, @prev, @stat com produtor permanente) ·
`scheduler/setok_bugs_test.go` (Set OK materializa saídas) ·
`api/bugs_behavior_test.go` (Set OK de WAITING; forced zera) ·
`api/holdall_delete_test.go` (hold geral/delete — sem claims) ·
`scheduler/explain_test.go` (WAIT_CONDITION p/ aresta legada; blockers `[]`).

## Checklist antes de mexer nesta área

1. Releia este doc (5 min) e o cabeçalho de `domain/conditions.go`.
2. Rode `go test ./internal/scheduler/ ./internal/api/` ANTES e DEPOIS.
3. Mudou semântica? Atualize **este doc + os testes no MESMO commit**.
4. Valide ao vivo com o par pai→filho (setinha cria as 3 listas; deletar no
   painel trava o filho; Set OK+rerun aguarda; rerun do pai destrava) — e
   **rebuilde server E front** antes de testar (binário velho engana).
