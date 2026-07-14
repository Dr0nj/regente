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

## Sistema 1 — Dependências do grafo = eventos consumíveis (schemaV15)

Duas tabelas (ver `server/internal/db/db.go` §schemaV15):

- **`dep_events`** — ledger IMUTÁVEL: todo término **terminal** de uma instance
  (OK, NOTOK pós-retries, Set OK) publica um evento `{def_id, instance_id,
  order_date, status}`. Rerun do pai = evento NOVO. Nada apaga eventos.
- **`dep_claims`** — o consumo: uma aresta satisfeita é um **claim** (latch) do
  consumidor sobre um evento livre. `UNIQUE(event_id, consumer_def_id)` = um
  evento satisfaz **no máximo uma instance de cada definition** consumidora.

### As regras (numeradas — cite pelo número em commits/testes)

**R1 — Emissão.** Término terminal publica evento (`emitDepEvent`, chamado no
`FinishInstance` e no `SetOK`). O status do evento decide que arestas satisfaz:
`on-success`/`""`→OK · `on-failure`→NOTOK · `on-complete`→ambos · `always` não
consome evento (basta o upstream existir no dia).

**R2 — Claim.** O pré-passe do tick (`claimDepEdges`) tenta latchar as arestas
de toda instance WAITING/HELD **antes** do gate de janela — a daily reserva o
evento do pai assim que ele termina, mesmo faltando horas pro horário. Ordem:
daily primeiro, forçadas depois (a cópia não rouba o evento da daily).

**R3 — Consumo materializa SÓ no OK.** Enquanto o consumidor não termina OK, o
claim é uma reserva. Quando ele termina **OK (real ou Set OK)**, o consumo é
**PERMANENTE**: a condição que liberou o job **SOME** — é o *delete input
conditions on OK* do Control-M.

**R4 — Falha/cancel DEVOLVEM.** NOTOK terminal e cancel do consumidor deletam
os claims dele (`ResetDepClaims`): a condição não foi gasta por uma execução
que não aconteceu de verdade. Um clone pendente — ou o rerun dele mesmo —
re-clama o evento devolvido **sem** rerun do pai.

**R5 — Rerun do consumidor (o bug de 2026-07-14).** `RerunDepClaims`, chamado
**ANTES** do reset de status (o status terminal decide o destino dos claims):

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
(`blockers` do Explain, `depsSatisfied`, …) serializa como `[]`, nunca `null`
— slice nil do Go derruba o front (`.some`/`.map` direto). Regressão coberta
em `TestExplain_Runnable`.

### Ação do operador × efeito nos eventos

| Ação | Evento do pool | Claims da instance |
|---|---|---|
| Job termina OK | emite evento novo (R1) | os que detinha ficam CONSUMIDOS (R3) |
| Job termina NOTOK terminal | emite evento novo (R1) | devolvidos ao pool (R4) |
| Set OK | emite evento novo (R8) | ficam consumidos (R8) |
| Cancel | — | devolvidos ao pool (R4) |
| Rerun (era OK) | — | LÁPIDE — evento segue gasto; novo run espera evento novo (R5) |
| Rerun (era NOTOK/CANCELLED) | — | já devolvidos; re-clama livre (R4/R5) |
| Rerun do PAI | novo término = evento novo | intocados no filho (R6) |
| Run Now | não consome (bypass) | intocados; `forced` zera no próximo rerun (R7) |
| Order Force | cópia disputa eventos LIVRES | claims próprios da cópia (R2/R7) |

### Onde está no código

- `server/internal/scheduler/depevents.go` — todo o motor (emit/claim/reset/
  rerun/backfill) + o comentário de cabeçalho espelhando estas regras.
- `server/internal/scheduler/scheduler.go` — `FinishInstance` (R1/R4),
  `SetOK` (R8), pré-passe no tick (R2).
- `server/internal/api/instances.go` + `bulk.go` — handlers de rerun/cancel
  (ordem: `RerunDepClaims` ANTES do reset de status), Run Now (R7).
- `server/internal/scheduler/explain.go` — gate + textos do WAIT EVENT (R10).
- `app/src/v2/canvas-layout.ts` — pintura da linha (`evaluateEdgeState`,
  claim-aware) e `isWaitingOnDeps` (mesma régua p/ menus).

### Testes que travam a semântica

`scheduler/depevents_test.go` (cenário-guia completo + rerun após OK/NOTOK/
Set OK) · `api/bugs_behavior_test.go` (confirmed sobrevive, forced zera,
Set OK de WAITING) · `scheduler/setok_bugs_test.go` (ConditionsOut no Set OK)
· `scheduler/explain_test.go` (blockers `[]`, nunca null).

---

## Sistema 2 — Conditions nomeadas (F16)

Estado global por nome + `scope_date` (tabela `conditions`;
`server/internal/scheduler/conditions.go`). **Não são consumíveis**: existem
até alguém remover (diferença deliberada do Sistema 1 — cobrem sinais externos
e fan-out N:M onde um estado libera muitos jobs sem contagem).

- **Gate**: job com `conditionsIn=[X,Y]` fica **WAIT CONDITION** até TODAS
  existirem no seu `order_date` (ou como permanentes, `scope_date=''`).
- **Quem cria/remove**: `conditionsOutAdd`/`conditionsOutRemove` de um job que
  termina **OK ou Set OK** (`ApplyOutcomes`) · ação On/Do **`set-condition`** ·
  evento externo (`POST /api/events/ingest`) · operador (UI/API/MCP).
- **Vínculo = nome exato.** O circuito produtor→consumidor é fechado na UI:
  Design drawer → aba **Dependências → Conditions** (editores de Entrada /
  Saída＋ / Saída−, com datalist do vocabulário do escopo e referência cruzada
  de quem cria/consome cada nome) · hint equivalente no editor On/Do ·
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

## Checklist antes de mexer nesta área

1. Releia este doc (5 min) e o cabeçalho de `depevents.go`.
2. Rode `go test ./internal/scheduler/ ./internal/api/` ANTES e DEPOIS.
3. Mudou semântica? Atualize **este doc + os testes + o comentário do
   depevents.go no MESMO commit**.
4. Valide ao vivo com o par pai→filho (rerun no filho OK tem que dar WAIT
   EVENT; rerun no pai tem que destravar) — e **rebuilde server E front**
   antes de testar (binário velho engana).
