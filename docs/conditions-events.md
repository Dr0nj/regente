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

## Lógica booleana de entrada — AND/OR (CL)

> **Status: TEMA FECHADO — CL-1…CL-6 entregues e validados. CL-1 (avaliador DNF),
> CL-2 (fallback `$TIME` + desacople do piso `windowFrom` + toggle na UI), CL-3
> (data model + imutabilidade), CL-4 (gate + Explain OR-aware + LINHAS OR do
> canvas), CL-5 (editor de grupos no drawer), CL-6 (bateria + docs + validação ao
> vivo).**
> Validação ao vivo (2026-07-20, servidor git-backed): construir `(A) OU (B)` no
> drawer persiste `conditionLogic` no YAML via round-trip completo (`conditionsIn`
> = união dos membros, `conditionsOutAdd` preservado, reabre limpo); e a **linha OR
> do canvas do Design** renderiza pontilhada com rótulo **"OU"** (baseline AND era
> tracejado sem rótulo). O visual OR do **Monitoring** usa o MESMO `makeEdge`/
> `condIsAlternative` a partir da coluna congelada `cond_logic` (schemaV21) —
> coberto por `TestList_SerializesCondLogic` e pelo código compartilhado.
> A UI é a aba **Condições** do drawer (Design): a entrada é lista plana (AND) por
> padrão e o toggle **AND/OR** revela o editor de GRUPOS (cada grupo com AND/OR +
> operador de topo — rótulos em inglês, o vocabulário canônico do `op:`; o rótulo
> das linhas OR do canvas é **"OR"**). A UI mantém `conditionsIn` = união dos
> membros; `conditionLogic` só existe no modo avançado.
> **Descobribilidade do CL-2 (2026-07-21, report "não achei o OR com horário"):**
> o atalho **"OR rodar no horário"** aparece SEMPRE que há UMA condição — com
> `windowFrom` ele cria a DNF `(C1) OU ($TIME)`; sem `windowFrom` ele fica
> esmaecido e o clique abre a aba **Horário** (o guard na UI segue: `$TIME` sem
> "A partir de" seria satisfeito imediatamente e anularia a condição). O
> **＋ horário** de cada grupo idem; um membro ⏱ órfão de `windowFrom` (janela
> apagada depois de adicionar o token) fica ÂMBAR com o aviso "satisfeito
> imediatamente".

A entrada de um job deixou de ser SÓ um AND implícito de `conditionsIn`: ganhou
um campo **opcional** `conditionLogic` — uma expressão booleana em **forma DNF**
(disjunção de conjunções, dois níveis, cada nível com seu operador):

```
conditionLogic:
  op: OR                      # operador de TOPO entre os grupos
  groups:
    - { op: AND, members: [C1, C2] }
    - { op: AND, members: [C3] }
```

- **Modelo canônico:** `topOp( grupoOp(membro ∈ pool?) for grupo in groups )`.
  - `(C1 AND C2) OR C3` → `op:OR, groups:[{AND,[C1,C2]},{AND,[C3]}]`.
  - `(C1 OR C2) AND C3` → `op:AND, groups:[{OR,[C1,C2]},{AND,[C3]}]`.
- **Semântica do OR:** o **primeiro ramo que chega satisfaz e dispara** — assim
  que QUALQUER grupo fica verdadeiro, o job roda (não espera os demais).
- **Membros** carregam o sufixo de data `@odat/@prev/@stat` como sempre. O token
  reservado **`$TIME`** é satisfeito quando `now >= scheduledAt` (mesmo relógio
  do gate de janela) — é a base do fallback temporal "condição OU horário"
  (CL-2); o gate já o avalia, mas o desacoplamento do piso `windowFrom` é CL-2.
- **Retrocompat (C1 continua valendo):** `conditionLogic` ausente/nil = **UM
  grupo AND** sobre `conditionsIn` — o gate antigo, byte a byte (um blocker por
  condição faltante). Com lógica, o gate bloqueia só se **NENHUM ramo é
  satisfazível** e emite UM blocker com a expressão (`RenderExpr`, ex.:
  "aguardando (C1 E C2) OU C3 — nenhum ramo satisfeito"); o Explain mostra o
  mesmo texto.
- **Invariante membros ⊆ `conditionsIn`:** todo membro não-`$TIME` também consta
  em `conditionsIn` (garantido por `NormalizeConditions` no chokepoint de
  leitura). Assim topologia (`upstream` derivado), linhas do Monitoring e a
  coluna congelada `conds_in` seguem lendo `conditionsIn` **sem saber da
  lógica** — só a AVALIAÇÃO do gate usa `conditionLogic`.
- **Membros AVULSOS = requisito AND:** um nome de `conditionsIn` que NÃO está em
  nenhum grupo da lógica é ANDado com a expressão inteira
  (`satisfeito = topOp(grupos) E todos-os-avulsos`). É o que faz a **setinha**
  "just work" num job com lógica: ela adiciona a condição simples em
  `conditionsIn` e ela vira obrigatória, sem precisar dobrar a aresta dentro de
  um OR ambíguo. A UI preserva os avulsos ao editar os grupos.
- **Imutabilidade M1:** `conditionLogic` é congelada no `definition_snapshot`
  como o resto da def (o gate lê via `defForInstance`); mudar a lógica na def
  viva NÃO relaxa uma instance já ordenada. O card/Explain leem a lógica
  CONGELADA. Ver [[regente-monitoring-immutable-snapshot]].
- **Linhas OR do canvas (CL-4):** uma aresta P→C é "alternativa" (OU) quando a
  condição que a liga é membro de um grupo OR na lógica do consumidor
  (`condIsAlternative` em `lib/conditions-model.ts`) — renderizada pontilhada +
  rótulo "OU" (`makeEdge(...,alt)` em `v2/canvas-layout.ts`). No **Design** vem da
  def viva (`buildDesignCanvas`); no **Monitoring** vem da coluna CONGELADA
  `cond_logic` (schemaV21) — `frozenMonitorCols`/`MigrateCondLogicSnapshot`/
  `instanceRow.CondLogic`/`JobInstance.condLogic` (imutável, nunca a def viva).
  A setinha sempre cria AND (linha sólida).
- **Código:** tipos + avaliador puro em `domain/conditions.go`
  (`ConditionLogic`, `CondGroup`, `EvalConditionLogic`, `RenderExpr`,
  `looseMembers`, `UsesTimeToken`); wiring do gate em `scheduler/explain.go`
  (`gateInstance`, `$TIME` desacopla o piso de janela). Front: tipos em
  `lib/orchestrator-model.ts`, round-trip em
  `lib/adapters/storage/ServerApiAdapter.ts`, editor (`EntryConditions`,
  `GroupedLogicEditor`, `GroupBox`, `OpToggle`) em `v2/JobConfigDrawer.tsx`.
  Testes: `domain/conditionlogic_test.go` (avaliador puro/DNF/`$TIME`/avulsos/
  normalização), `scheduler/condlogic_gate_test.go` (gate+Explain+`$TIME`+
  imutabilidade) e `api/bugs_behavior_test.go::TestList_SerializesCondLogic`.

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
contra as defs vivas). **`applyConditionsOut` é SNAPSHOT-ONLY (M1, 2026-07-17):**
as saídas aplicadas no OK/Set OK vêm SÓ da def congelada na ordem — criar um
consumidor novo no Design depois da ordem NÃO faz o OK de hoje produzir a
condição nova (só a próxima ordem Force/daily carrega o OutAdd novo). A união
snapshot∪def-viva que existia por compat pré-unificação foi CONGELADA nos
snapshots em voo uma única vez no upgrade (`MigrateMonitoringSnapshot`,
meta_flags `monitoring-snapshot-v18`); def viva só como fallback de instance
legada SEM snapshot. Backfill one-time da unificação no boot
(`MigrateConditionsUnify`, flag em `meta_flags` schemaV17): par
(pai já OK, filho WAITING não-consumido) semeia a condição no pool.
**dep_events/dep_claims (schemaV15) estão APOSENTADAS** — tabelas ficam no
banco por compat, nada lê/escreve.

## Linhas do Monitoring — reflexo do pool

Topologia = **matching instance-a-instance pelos SNAPSHOTS da ordem** (M1,
2026-07-17): cada instance carrega `condsIn`/`condsOutAdd` congelados
(schemaV18) e a linha existe quando o `condsOutAdd` de uma cópia produz uma
entrada do `condsIn` da outra, respeitando o sufixo de data (`@odat` mesma
origem · `@prev` diária anterior · `@stat` qualquer — carregado do 14 só ganha
linha com o pai do 14). Criar/ligar jobs novos no Design NÃO redesenha
instances já ordenadas; a topologia viva (`def.upstream` derivado) é só do
DESIGN. A COR é o estado das condições que LIGAM o par para ESTE consumidor:

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
6. Monitoring derivando QUALQUER coisa de card da def viva (M1: label, tipo,
   linhas, `waitConfirm`, `waitAgent` e as saídas do `applyConditionsOut` são
   TODOS do snapshot/colunas congeladas; o WAIT COND lê o POOL, que é estado
   de runtime, não def — e a lista de entradas que ele checa é a congelada).
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
  `FinishInstance`/`SetOK` → `applyConditionsOut` (snapshot-only, M1),
  `defForInstance` (expansão de snapshot), `ForceOrder`.
- `server/internal/scheduler/condmigrate.go` — backfill one-time (meta_flags).
- `server/internal/scheduler/monitorsnapshot.go` — colunas congeladas schemaV18
  (`frozenMonitorCols`) + `MigrateMonitoringSnapshot` (backfill + congelamento
  da união produtor nos snapshots em voo).
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
