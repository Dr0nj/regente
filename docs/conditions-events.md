# Condições e eventos — especificação de comportamento (FONTE ÚNICA)

> **LEIA ESTE ARQUIVO INTEIRO antes de tocar em qualquer código de dependência,
> evento, condition, rerun, Set OK ou Force.** Ele é a fonte única da semântica;
> o roadmap registra *quando* cada regra entrou, mas o comportamento canônico é
> o daqui. Se um patch contradiz este doc, ou o patch está errado ou este doc
> precisa mudar JUNTO (no mesmo commit, com o porquê).

O Regente tem **dois sistemas de dependência**, deliberadamente diferentes:

| | Sistema 1 — setas do grafo | Sistema 2 — conditions nomeadas (F16) |
|---|---|---|
| Declaração | `upstream: [{from, condition}]` | `conditionsIn` / `conditionsOutAdd` / `conditionsOutRemove` |
| Natureza | **EVENTO consumível** (some quando gasto) | **Estado nomeado** (existe até alguém remover) |
| Vínculo | id da definition upstream | **nome exato** da condition |
| Gate na UI | WAIT EVENT | WAIT CONDITION |
| Paralelo Control-M | conditions com *delete input conditions on OK* sempre ligado | global conditions clássicas |

---

## Datas — ODAT / PREV / STAT (2026-07-16)

**ODAT** = a data de ORIGEM de uma ordem (Control-M ODATE): o dia em que ela
entrou em schedule pela primeira vez. O carry-over da virada AVANÇA
`instances.order_date` (o "dia ativo" que tick/API/RBAC filtram) preservando a
origem em `carried_from`:

```
ODAT = COALESCE(NULLIF(carried_from,''), order_date)      (scheduler/odate.go)
```

**TODO escopo de data usa ODAT, nunca o order_date avançado.** Um job
carregado do dia 14 continua "do dia 14": disputa os eventos do dia 14, cria
conditions do dia 14 e aparece agrupado sob 14 na sidebar. (Antes o carregado
disputava os eventos dos jobs FRESCOS do dia — report do usuário.)

Referências de data, sempre relativas ao ODAT do CONSUMIDOR/produtor:

| ref | Sistema 1 (aresta `dateRef`) | Sistema 2 (sufixo no nome) | significado |
|---|---|---|---|
| **odat** (default) | `dateRef:` omitido/`odat` | sem sufixo / `NOME@odat` | mesma diária de origem |
| **prev** | `dateRef: prev` | `NOME@prev` | diária ANTERIOR = último New Day registrado em `daily_runs` antes do ODAT (cobre lacunas de server desligado); sem registro, ODAT−1 dia |
| **stat** | `dateRef: stat` | `NOME@stat` | estática: sem data — qualquer evento livre (S1) / só a condition permanente `scope_date=''` (S2) |

## Hold GERAL + Delete (2026-07-16)

**Hold vale pra QUALQUER status exceto RUNNING** (a execução já está no agente
— cancele ou aguarde) e o próprio HELD. `instances.held_from_status`
(schemaV16) congela o status original; **Release/Resume restauram ELE**, não
WAITING cego (que re-executaria um OK segurado). Vale pro hold individual, pro
bulk e pra **pausa de folder — que agora segura o dia INTEIRO, carry-over
incluso** (o carry avança `order_date`, então a carregada entra no WHERE do
dia). Hold individual continua sobrevivendo ao pause/resume da folder
(`hold_scope`, schemaV14).

**Delete (Control-M "Delete job")** — `DELETE /api/instances/{id}` e ação
`delete` do bulk: remove a ordem da tela e do state store (instance +
instance_events; o ledger `dep_events` fica). **SÓ com o job em HOLD** — como
RUNNING não é segurável, job em execução nunca é deletável; o fluxo é
Hold → Delete. Broadcast `instance.deleted` (o espelho do front remove via
tombstone).

Interação com os claims — **`SettleDepClaims`** (rerun/cancel/delete): o HELD é
um véu, não um término — o que decide é `held_from_status`:

- efetivo **OK** (real, Set OK, ou OK segurado) → consumo é PERMANENTE: claims
  viram LÁPIDE (`#spent@`), o evento segue gasto pra definition (invariante 2).
  No delete a linha do claim simplesmente sobrevive à instance — o
  `UNIQUE(event_id, consumer_def_id)` continua bloqueando.
- qualquer outro → reserva não-consumida: claims deletados, eventos ao pool (R4).

E o pré-passe de claims do tick **pula HELD vindo de status terminal**
(OK/NOTOK/CANCELLED segurados não disputam eventos — só WAITING de verdade ou
HELD-de-WAITING/legado clama).

## Ciclo de vida da daily (carry-over) — quem atravessa a virada

Regra pura em `carryDecision` (scheduler.go); idades em **DIAS-CALENDÁRIO**
(timezone da daily), então New Day que não rodou NÃO estica vida de ninguém
(14→16 com o 15 pulado = 2 dias):

| estado na virada | atravessa? |
|---|---|
| RUNNING | SEMPRE (rastrear até terminar) |
| HELD | SEMPRE, enquanto em hold (inclusive OK/NOTOK segurados pelo hold geral) |
| NOTOK | enquanto `dias desde a FALHA ≤ keepActive\|1` (um NOTOK não-tratado persiste +1 diária) |
| WAITING com retry AGENDADO (D-1: `attempts>1` **e** `started_at` preenchido) | regra do NOTOK (falha em tratamento) |
| WAITING (incl. aguardando CONFIRM) | `dias desde o ODAT ≤ keepActive` — **keepActive=0 morre na 1ª virada** |
| OK / CANCELLED | nunca |

Rerun de OPERADOR zera `started_at` (handlers de rerun) — por isso ele NÃO
conta como "retry em tratamento": um job re-rodado e esquecido em WAITING
obedece keepActive estrito.

## Sistema 1 — Dependências do grafo = eventos consumíveis (schemaV15)

Duas tabelas (ver `server/internal/db/db.go` §schemaV15):

- **`dep_events`** — ledger IMUTÁVEL: todo término **terminal** de uma instance
  (OK, NOTOK pós-retries, Set OK) publica um evento `{def_id, instance_id,
  order_date, status}` — `order_date` do evento = **ODAT do produtor** (a
  origem, não o dia ativo). Rerun do pai = evento NOVO. Nada apaga eventos.
- **`dep_claims`** — o consumo: uma aresta satisfeita é um **claim** (latch) do
  consumidor sobre um evento livre **da diária resolvida pelo `dateRef` da
  aresta contra o ODAT do consumidor** (odat/prev/stat, ver §Datas).
  `UNIQUE(event_id, consumer_def_id)` = um evento satisfaz **no máximo uma
  instance de cada definition** consumidora.

### As regras (numeradas — cite pelo número em commits/testes)

**R1 — Emissão.** Término terminal publica evento (`emitDepEvent`, chamado no
`FinishInstance` e no `SetOK`). O status do evento decide que arestas satisfaz:
`on-success`/`""`→OK · `on-failure`→NOTOK · `on-complete`→ambos · `always` não
consome evento (basta o upstream existir no dia).

**R2 — Claim.** O pré-passe do tick (`claimDepEdges`) tenta latchar as arestas
de toda instance WAITING/HELD **antes** do gate de janela — a daily reserva o
evento do pai assim que ele termina, mesmo faltando horas pro horário. Ordem:
daily primeiro, forçadas depois (a cópia não rouba o evento da daily). HELD
vindo de status TERMINAL (hold geral, `held_from_status`) fica de fora — um
NOTOK/OK segurado não disputa evento (§Hold GERAL + Delete).

**R3 — Consumo materializa SÓ no OK.** Enquanto o consumidor não termina OK, o
claim é uma reserva. Quando ele termina **OK (real ou Set OK)**, o consumo é
**PERMANENTE**: a condição que liberou o job **SOME** — é o *delete input
conditions on OK* do Control-M.

**R4 — Falha/cancel DEVOLVEM.** NOTOK terminal deleta os claims do consumidor
(`ResetDepClaims`); cancel passa pelo `SettleDepClaims` (2026-07-16), que
devolve tudo que NÃO era um consumo OK: a condição não foi gasta por uma
execução que não aconteceu de verdade. Um clone pendente — ou o rerun dele
mesmo — re-clama o evento devolvido **sem** rerun do pai. (Cancel de um OK —
direto ou segurado por hold — cai no R5: consumo OK nunca volta pro pool.)

**R5 — Rerun/cancel/delete do consumidor (o bug de 2026-07-14; hold geral
2026-07-16).** `SettleDepClaims`, chamado **ANTES** do reset/remoção de status
(o status pré-ação decide o destino dos claims — e HELD é um véu: vale o
`held_from_status` congelado pelo hold):

- run anterior **OK** → claims viram **LÁPIDE** (tombstone): o
  `consumer_instance_id` ganha sufixo `#spent@<event>`. A linha do novo run
  reseta (o `claimsFor`/`DepsSatisfied` não a vê mais), mas o
  `UNIQUE(event_id, consumer_def_id)` continua valendo — **o evento segue
  gasto para esta definition**. O rerun entra em **WAIT EVENT** até o pai
  emitir um término novo. *Deletar o claim aqui era o bug: o evento gasto
  voltava pro pool e o próprio rerun o re-clamava e rodava na hora.*
- qualquer outro status → não consumiu: claims deletados, eventos ao pool (R4).

**R6 — Rerun/cancel do PAI não tocam claim de ninguém.** A linha verde do
Monitoring é a HISTÓRIA da instance consumidora, não o estado vivo do pai.

**R7 — Force.** `Order Force` (Design, `force_mode='order'`) cria ordem NOVA
que bypassa SÓ o agendamento — deps por evento valem: se o término do pai já
foi consumido, a cópia nasce em WAIT EVENT. `Run Now` (Monitoring,
`force_mode=''`) força a instance EXISTENTE com bypass total (não claima nada).
**O bypass do Run Now NÃO é pegajoso**: rerun de instance com `forced=1 &&
mode=''` zera o `forced` — o rerun volta ao gating normal (quer bypass de
novo? Run Now de novo). A cópia de Order Force MANTÉM `forced` no rerun (ela
nunca teve janela real; o mode='order' já submete aos gates de runtime).

**R8 — Set OK.** Nos dois papéis, é um término OK de verdade:
- como **predecessor**: emite o dep-event (destrava on-success de pai falho) e
  materializa as ConditionsOut (F16) — BUG-3/4;
- como **sucessor**: os claims que ele detinha ficam gastos (R3/R5 — rerun
  depois de Set OK entra em WAIT EVENT igual a OK real). Se ainda não tinha
  claim, o "reconsumo lazy" (R9) pode tomar um evento na próxima disputa.

**R9 — Backfill/compat.** Instances terminadas antes da feature não têm
eventos: `tryClaimEdge` materializa lazy o evento implícito e dá o claim
primeiro a quem JÁ RODOU sem falhar (RUNNING/OK — consumo histórico). NOTOK e
CANCELLED ficam de fora de propósito (R4: o backfill não pode entregar o
evento de volta ao falho). Set OK reconverte em OK → reconsome lazy.

**R10 — JSON nunca null.** Toda lista que a API expõe desse sistema
(`blockers` do Explain, `depsSatisfied`, `depsClaims`, …) serializa como `[]`, nunca `null`
— slice nil do Go derruba o front (`.some`/`.map` direto). Regressão coberta
em `TestExplain_Runnable`.

### Ação do operador × efeito nos eventos

| Ação | Evento do pool | Claims da instance |
|---|---|---|
| Job termina OK | emite evento novo (R1) | os que detinha ficam CONSUMIDOS (R3) |
| Job termina NOTOK terminal | emite evento novo (R1) | devolvidos ao pool (R4) |
| Set OK | emite evento novo (R8) | ficam consumidos (R8) |
| Cancel (não era OK) | — | devolvidos ao pool (R4) |
| Cancel (era OK, mesmo sob hold) | — | LÁPIDE — consumo OK é permanente (R5) |
| Rerun (era OK, mesmo sob hold) | — | LÁPIDE — evento segue gasto; novo run espera evento novo (R5) |
| Rerun (era NOTOK/CANCELLED) | — | já devolvidos; re-clama livre (R4/R5) |
| Hold / Release | — | intocados (o hold congela, não gasta nem devolve) |
| Delete (era OK sob hold) | — | LÁPIDE — a linha do claim sobrevive à instance (R5) |
| Delete (reserva não-consumida) | devolvido ao pool | deletados (R4) |
| Rerun do PAI | novo término = evento novo | intocados no filho (R6) |
| Run Now | não consome (bypass) | intocados; `forced` zera no próximo rerun (R7) |
| Order Force | cópia disputa eventos LIVRES | claims próprios da cópia (R2/R7) |

### Linhas do Monitoring — por PAR de instances (2026-07-14; datas 2026-07-16)

O dia pode ter VÁRIAS cópias do mesmo job (Order Force/rerun) **e de VÁRIAS
origens** (carry-over). A dependência declarada existe entre os pares **da
diária que o `dateRef` aceita** (`parentsForEdge` no canvas-layout: odat =
mesmo ODAT; prev = ODAT anterior ao do filho; stat = todas) — o carregado do
dia 14 só ganha linha com o pai do dia 14, nunca com o fresco de hoje. Dentro
dos pares elegíveis, o canvas desenha **uma linha de cada cópia do pai para
cada cópia do filho**. A cor diz o papel do par:

- **verde ✓** — ESTA cópia do pai emitiu o evento que ESTE consumidor clamou.
  O detalhe vem da API: `depsClaims [{from, parentInstanceId}]` ao lado do
  `depsSatisfied` (attachDepClaims junta `dep_claims × dep_events`).
- **cinza** — a dependência existe mas este par não a satisfez: pendente, ou o
  consumidor já foi satisfeito por OUTRA cópia (neutro de propósito — inclusive
  se esta cópia falhar depois: o latch é história, R6).
- **vermelho ✗** — esta cópia do pai falhou/cancelou e o consumidor ainda não
  foi satisfeito por ninguém.

O CARD (badge WAIT EVENT) segue **def-level**: um evento de QUALQUER cópia do
pai libera o consumidor (R2) — a linha é informação por par, o gate não.

### Onde está no código

- `server/internal/scheduler/odate.go` — ODAT (`odateExpr`, `instRow.Odate()`),
  `prevDaily`, `resolveEdgeDate` (dateRef S1), `splitCondRef` (sufixos S2),
  `daysBetween` e o índice `(def, odate)` do gate (`instIndex`).
- `server/internal/scheduler/depevents.go` — todo o motor (emit/claim/reset/
  settle/backfill; `SettleDepClaims` lê o `held_from_status` sob o HELD) + o
  comentário de cabeçalho espelhando estas regras.
- `server/internal/scheduler/scheduler.go` — `FinishInstance` (R1/R4),
  `SetOK` (R8), pré-passe no tick (R2), `carryDecision`/`carryOver` (ciclo de
  vida da daily).
- `server/internal/api/instances.go` + `bulk.go` — handlers de hold/release
  (hold geral: `held_from_status`, `releaseSQL` restaura), delete (só HELD),
  rerun/cancel (ordem: `SettleDepClaims` ANTES do reset de status), Run Now
  (R7), `attachDepClaims` (`depsSatisfied` + `depsClaims` com o produtor).
- `server/internal/api/workflow.go` — pausa/resume de folder (dia inteiro,
  carry-over incluso; resume restaura o status original).
- `server/internal/scheduler/explain.go` — gate + textos do WAIT EVENT (R10).
- `app/src/v2/canvas-layout.ts` — pintura da linha POR PAR (`evaluateEdgeState`,
  claim-aware via `depsClaims`), régua def-level do card (`isDepSatisfied`) e
  `isWaitingOnDeps` (mesma régua p/ menus; recebe TODAS as cópias por def).

### Testes que travam a semântica

`scheduler/depevents_test.go` (cenário-guia completo + rerun após OK/NOTOK/
Set OK) · `scheduler/lifecycle_test.go` (carryDecision por status/idade em
dias, gap de New Day 14→16, rerun de operador ≠ retry, **TestOdat_*: escopo
ODAT de eventos, dateRef prev/stat, sufixos @odat/@prev/@stat e ConditionsOut
no ODAT do produtor**) · `api/bugs_behavior_test.go` (confirmed sobrevive,
forced zera, Set OK de WAITING) · `api/holdall_delete_test.go` (hold geral:
release restaura o status original, RUNNING rejeitado, pausa de folder pega
todos os status + carry-over, delete só em HOLD, claims de OK segurado seguem
gastos) · `scheduler/depevents_test.go::TestDepEvents_HeldFromTerminalDoesNotClaim`
(HELD-de-terminal não disputa evento) · `scheduler/setok_bugs_test.go`
(ConditionsOut no Set OK) · `scheduler/explain_test.go` (blockers `[]`, nunca
null) · `api/depclaims_api_test.go` (depsClaims aponta a cópia certa do pai;
`[]` nunca null).

---

## Sistema 2 — Conditions nomeadas (F16)

Estado global por nome + `scope_date` (tabela `conditions`;
`server/internal/scheduler/conditions.go`). **Não são consumíveis**: existem
até alguém remover (diferença deliberada do Sistema 1 — cobrem sinais externos
e fan-out N:M onde um estado libera muitos jobs sem contagem).

- **Gate**: job com `conditionsIn=[X,Y]` fica **WAIT CONDITION** até TODAS
  existirem **no escopo resolvido do sufixo de cada nome contra o ODAT do
  job** (ver §Datas): sem sufixo/`@odat` = diária de ORIGEM (ou permanente);
  `@prev` = diária anterior; `@stat` = só a permanente (`scope_date=''`).
- **Quem cria/remove**: `conditionsOutAdd`/`conditionsOutRemove` de um job que
  termina **OK ou Set OK** (`ApplyOutcomes` — **no ODAT do produtor**, não no
  dia ativo avançado; `@stat` cria/remove a permanente) · ação On/Do
  **`set-condition`** (idem, ODAT + sufixos) · evento externo
  (`POST /api/events/ingest`, data explícita do emissor) · operador (UI/API/
  MCP, data explícita).
- **Vínculo = nome exato.** O circuito produtor→consumidor é fechado na UI:
  Design drawer → aba **Dependências → Conditions** (editores de Entrada /
  Saída＋ / Saída−, com datalist do vocabulário do escopo, referência cruzada
  de quem cria/consome cada nome — pelo nome-BASE, ignorando sufixo — e, desde
  2026-07-16, **seletor de data Odate/Prev/Stat por linha** que edita o sufixo
  sem digitar arroba, em entrada E saída) · hint equivalente no editor On/Do ·
  YAML (modo CODE) · mass-update (`add-condition-in`).
- Setar condition **cutuca o tick** (BUG-10/11): quem estava só esperando ela
  roda na hora.

---

## Invariantes (o que NUNCA pode voltar a acontecer)

1. Rerun de consumidor que terminou OK **re-rodando sem evento novo do pai**
   (bug 2026-07-14; R5).
2. Evento consumido por execução OK **voltando pro pool** por qualquer caminho
   (rerun, cancel do pai, backfill; R3/R6/R9).
3. Um mesmo evento satisfazendo **duas instances da mesma definition** (R2).
4. Bypass de Run Now **sobrevivendo a um rerun** (R7).
5. API mandando **`null` onde o front espera lista** (R10).
6. Monitoring derivando estado de card da **def viva** (é snapshot — ver
   memória do projeto; exceções deliberadas: `waitConfirm`/`waitAgent`).
7. Evento/condition de uma ORIGEM satisfazendo consumidor de OUTRA origem
   sem `dateRef`/sufixo pedindo (bug 2026-07-16: o job carregado do dia 14
   disputava os eventos dos frescos de hoje — §Datas).
8. WAITING com `keepActive=0` (inclusive aguardando CONFIRM) **atravessando a
   virada da daily** — e New Day pulado contando como "não passou dia"
   (idades em dias-calendário; §Ciclo de vida).

## Checklist antes de mexer nesta área

1. Releia este doc (5 min) e o cabeçalho de `depevents.go`.
2. Rode `go test ./internal/scheduler/ ./internal/api/` ANTES e DEPOIS.
3. Mudou semântica? Atualize **este doc + os testes + o comentário do
   depevents.go no MESMO commit**.
4. Valide ao vivo com o par pai→filho (rerun no filho OK tem que dar WAIT
   EVENT; rerun no pai tem que destravar) — e **rebuilde server E front**
   antes de testar (binário velho engana).
