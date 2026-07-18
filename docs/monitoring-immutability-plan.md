# Plano — Imutabilidade TOTAL do Monitoring (Active Jobs File de verdade)

> **Status: ✅ IMPLEMENTADO (2026-07-18).** Etapas E1–E5 entregues, testadas (Go +
> `tsc`/`vite`) e VALIDADAS AO VIVO em server real offline. Este doc fica como registro
> do racional e do mapa de vazamentos. O que foi feito, resumido:
> - **E1** — schemaV18 (`label`/`job_type`/`confirm_req`/`environment`/`pinned_agent`/
>   `conds_in`/`conds_out_add` na `instances`) + `frozenMonitorCols`/`producedConds`
>   gravando nos INSERTs (RunDaily/ForceOrder) + backfill one-time `MigrateMonitoringSnapshot`
>   (`monitorsnapshot.go`) + colunas em `instanceCols`/`scanInstances` (`instances.go`).
> - **E2** — `applyConditionsOut` snapshot-only (união produtor congelada no upgrade pelo
>   mesmo backfill).
> - **E3** — `buildMonitoringCanvas` 100% instance-driven (edges por matching de `condsIn`×
>   `condsOutAdd` congelados; `waitConfirm`/`waitAgent`/`isWaitingOnConds` pelos campos da
>   instance; helpers `inst*` em `conditions-model.ts`). **Sharp-edge corrigido na validação:
>   NUNCA testar "label vazio" com `label !== definitionId`** — um label congelado legítimo
>   pode ser igual ao id (era a raiz do card mostrar o nome NOVO da def viva); `toWeb` deixa
>   label vazio pro legado e o fallback é `inst.label || def.label`.
> - **E4** — drawer lê `snapshotDef` (def congelada inteira) do `GET /instances/{id}`.
> - **E5** — `monitorsnapshot_test.go` (rename congelado · Force convive · consumidor novo
>   não retroage · daily seguinte pega def nova · backfill) + validação ao vivo.
>
> Histórico original (levantamento de 2026-07-17) abaixo — mantido como referência.

## O contrato (pedido do usuário, 2026-07-17 — é a semântica Control-M do Active Jobs File)

- O Monitoring é a **foto da ordem**. Nome (label), tipo, condições, gates, action: TUDO
  congela na hora que a instance entra (daily ou force). **Publicar mudança no Design NÃO
  altera NADA de card já ordenado** — nem no F5, nem no live update.
- Exemplo canônico do report: `job0002` ordenado à meia-noite; renomeado no Design para
  `job0002-b` durante o dia → o card do Monitoring fica **a diária inteira** como
  `job0002`. Um **Force** depois do rename cria ordem NOVA com a def publicada ATUAL
  (`job0002-b`) e **as duas convivem** no board. No dia seguinte, só a nova entra.
- Jobs novos criados durante o dia (`job0003`/`job0004` dependendo do `job0002`) **não
  mudam nada** do `job0002` já ordenado: nem linhas novas no grafo, nem comportamento —
  o OK do `job0002` de hoje **NÃO cria** `JOB0002-TO-JOB0003` (o snapshot dele não tem
  essa saída). O `job0003` forçado hoje fica **WAIT COND** até um operador setar a
  condição no painel (ou forçar o pai de novo — a cópia forçada do pai JÁ tem a saída
  nova no snapshot dela). É exatamente a paridade Control-M; o usuário confirmou que
  quer esse comportamento.

## O que JÁ é imutável (conquistado antes — NÃO mexer)

- **Gate + execução**: `defForInstance` (`server/internal/scheduler/scheduler.go:1046-1065`)
  resolve a def do snapshot p/ o tick inteiro — conditions IN, Confirm, agente, janela,
  action/dispatch. A mudança de Design **já não muda** o que a instance de hoje executa.
- Colunas congeladas existentes: `dry_run` (schemaV9, selo 👻GHOST) e `team`.
- **Force pega a def viva AO ORDENAR** (`ForceOrder`, scheduler.go:1736+) — correto, é a
  ordem nova; não tocar.
- Drawer de detalhe: label/jobType/actionConfig vêm do `definition_snapshot` via
  `GET /api/instances/{id}` (fix fab6de9).

## Os vazamentos (levantados em 2026-07-17, com arquivo:linha)

- **V1 — lista sem label/job_type** → front completa da def VIVA. A API de lista
  (`server/internal/api/instances.go:16-53`, `instanceRow`/`instanceCols`) não devolve
  label nem jobType; `buildMonitoringCanvas` funde da def viva
  (`app/src/v2/canvas-layout.ts:513-525`). **É a raiz do "renomeei/mudei o tipo de
  COMMAND pra SSH e o Monitoring mudou no F5".**
- **V2 — edges/topologia do Monitoring derivadas das defs VIVAS**
  (`canvas-layout.ts:546-573`): o loop usa `def.upstream` (visão derivada recalculada por
  name-matching sobre as defs vivas) e `isWaitingOnConds(inst, def_viva, pool)`. Criar
  `job0003`/`job0004` no Design redesenha as linhas de instances já ordenadas — o report
  "criei jobs e as novas condições já aparecem direto no monitoring".
- **V3 — flags de gate visuais pela def viva**: `waitConfirm`
  (`canvas-layout.ts:625-626`, lê `defsById.get(...).confirm`) e `waitAgent`
  (`canvas-layout.ts:619-620`, `hasAgentFor(defsById.get(...), ...)`).
- **V4 — lado PRODUTOR das condições faz união com a def viva**:
  `applyConditionsOut` (`server/internal/scheduler/scheduler.go:1672-1711`) aplica
  `snapshot ∪ def viva` em OutAdd/OutRemove. Um OutAdd criado no Design DEPOIS da ordem
  ainda é aplicado quando a instance de hoje termina OK. (A união existia por UM motivo:
  instances pré-unificação cujo snapshot não tinha o OutAdd sintetizado do upstream —
  ver comentário no código e `docs/conditions-events.md`.)
- **V5 — drawer do Monitoring, abas Schedule e Condições leem a def viva**
  (`InstanceDetailsDrawer`). Foi decisão explícita na época ("design read-only, PEDIDO");
  o pedido de 2026-07-17 REVERTE: tudo do snapshot.

## Decisões já tomadas (não re-discutir com o usuário)

1. **Colunas snapshotadas novas (schemaV18)** em vez de parsear `definition_snapshot`
   por request — a lista @1M não pode fazer JSON.parse por linha.
2. **`applyConditionsOut` vira snapshot-only.** Compat: migração one-time no boot congela
   a união vigente DENTRO do snapshot das instances não-finais (não muda semântica no
   meio do dia do upgrade); a partir daí, puro snapshot.
3. **Drawer Schedule/Condições → snapshot** (reversão consciente da decisão antiga).
4. **waitConfirm/waitAgent → campos snapshotados** (não mais "mesma leitura que o gate" —
   o gate JÁ é snapshot via defForInstance, então snapshot é que É a mesma leitura).
5. No Monitoring o front **não consulta mais `defsById` para NADA de instance**; defs
   vivas ficam só para o Design. Fallback à def viva apenas quando o campo novo vier
   vazio (linha pré-migração) — após o backfill isso não deve acontecer.

## Etapas

### E1 — server: colunas congeladas (schemaV18)

- `internal/db/db.go`: migração v18 — `ALTER TABLE instances ADD COLUMN`:
  `label TEXT NOT NULL DEFAULT ''`, `job_type TEXT NOT NULL DEFAULT ''`,
  `confirm_req INTEGER NOT NULL DEFAULT 0`, `environment TEXT NOT NULL DEFAULT ''`,
  `pinned_agent TEXT NOT NULL DEFAULT ''`, `conds_in TEXT NOT NULL DEFAULT ''`,
  `conds_out_add TEXT NOT NULL DEFAULT ''` (os dois últimos = JSON array de strings COM
  sufixos `@odat/@prev/@stat`; `conds_in` já EXPANDIDO — passar o snapshot por
  `domain.ExpandSnapshotConditions` na hora de gravar, cobrindo upstream legado).
- **Backfill** na própria migração (padrão `condmigrate.go`/meta_flags): para toda
  instance com `definition_snapshot != ''` e `label = ''`, parsear e preencher as 7
  colunas. `pinned_agent` = o MESMO resolvedor que o dispatch usa (def.AgentID /
  `Params["_agentId"]` — conferir no código do dispatch qual campo vale hoje e reusar).
  Snapshot vazio (legado antigo) → preencher da def viva UMA vez (congela agora).
- Escrever as colunas em TODO INSERT de instances: `RunDaily` (`scheduler.go:976`),
  `ForceOrder` (`scheduler.go:1757`) e **grep por `INSERT INTO instances`** para não
  esquecer nenhum caminho (cyclic? importctm? api?). Testes que inserem na mão podem
  ficar sem as colunas (DEFAULT cobre).
- `internal/api/instances.go`: `instanceCols` + `instanceRow` + `scanInstances` ganham
  `label`, `jobType`, `confirmReq`, `environment`, `pinnedAgent`, `condsIn []string`,
  `condsOutAdd []string` (decode do JSON no scan; array vazio quando ''). Lista, /page e
  tudo que passa por `scanInstances` ganham de graça.
- **WS `instance.bulk`**: conferir como o broadcast monta a linha — se não passa por
  `scanInstances`, incluir os campos lá TAMBÉM (senão o F5 mostra certo e o live update
  "des-corrige": o fallback do front à def viva reacenderia o bug).

### E2 — server: `applyConditionsOut` snapshot-only

- Remover a união com a def viva (`scheduler.go:1699-1707`); manter a def viva SÓ quando
  `snapshot == ""`. Atualizar o comentário 1672-1681 e o trecho correspondente de
  `docs/conditions-events.md` ("o lado produtor vem da def viva" morre).
- Migração one-time (meta_flags, mesmo padrão da unificação): para instances
  `WAITING/HELD/RUNNING` com snapshot, reescrever `definition_snapshot` com
  `ConditionsOutAdd/OutRemove = união(snapshot, def viva ATUAL)` — congela a semântica
  vigente uma única vez no upgrade.
- Testes existentes que dependem da união (`scheduler_test.go:139`,
  `setok_bugs_test.go:26` — setam `s.defs` esperando que a viva conte) **mudam de
  contrato**: reescrever para o snapshot.

### E3 — front: `buildMonitoringCanvas` 100% instance-driven

- Tipos: `JobInstance` ganha `condsIn`/`condsOutAdd`/`confirmReq`/`environment`/
  `pinnedAgent` — mapear em `ServerApiAdapter.toWeb` E `server-instance-store`
  (**gotcha histórico: o adapter DROPAVA campos ricos** — não repetir; e o merge
  `keepDetail` não pode sobrescrever os campos novos com vazio).
- Enriquecimento `canvas-layout.ts:516-525`: vira fallback SÓ para campo vazio.
- **Edges**: substituir o loop `def.upstream` por matching instance-a-instance: para cada
  inst e cada condição de `inst.condsIn` (nome-base), produtores = instances cujo
  `condsOutAdd` contém a base — respeitando o sufixo de data COMO `parentsForEdge` faz
  hoje (`@odat` mesma origem, `@prev` diária anterior, `@stat` qualquer). Portar
  `edgeCondNames`/`parentsForEdge` para versões por-instance (os atuais morrem junto com
  o loop). Cor da linha (`evaluateEdgeState` contra o pool) fica como está.
- `isWaitingOnConds` passa a usar `inst.condsIn`.
- `waitConfirm`: `inst.status==="WAITING" && !inst.confirmed && inst.confirmReq`.
- `waitAgent`: `hasAgentFor` passa a receber os campos crus
  (`{jobType, pinnedAgent, environment}` da instance) em vez da def.
- `buildDesignCanvas` continua nas defs vivas — é o lugar delas.

### E4 — front: `InstanceDetailsDrawer` todo no snapshot

- Abas Schedule e Condições leem do `definition_snapshot` do detalhe
  (`GET /api/instances/{id}` já devolve; conferir o shape no adapter). Remover a leitura
  da def viva. (Reversão da decisão antiga — registrar na memória do agente.)

### E5 — testes + validação ao vivo

- Go novos: **(a)** rename de label + troca de jobType APÓS ordenar → lista devolve os
  antigos; **(b)** Force após o rename → instance nova com label novo, as duas convivem;
  **(c)** consumidor novo criado após a ordem → OK do pai NÃO cria a condição nova
  (E2); **(d)** RunDaily do dia seguinte → pega a def nova.
- Front: `tsc -b && vite build` + roteiro ao vivo (server completo em porta alta, bare
  repo local, demo-mode): ordenar; renomear+trocar tipo+publicar; F5 → card intacto;
  criar job dependente novo → nenhuma linha nova no card antigo e OK do pai não cria a
  cond; Force → dois cards convivendo (linha do par novo aparece SÓ no forçado — o
  snapshot dele tem a saída nova: correto); RunDaily de amanhã → só a def nova.

### Pegadinhas conhecidas (dos levantamentos de hoje)

- `instance.bulk`/WS: ver E1 último bullet — é o furo mais provável de deixar passar.
- Payload da lista: `conds_*` só pesam em jobs com deps; a coluna `output` já é maior.
  O modo windowed (sidebar @1M) usa /page — mesmas colunas, sem trabalho extra.
- Modo local (localStorage): conferir se `createInstance` local já congela
  label/jobType/conds na instance (alinhar com o server; o tick local é outra
  implementação).
- Carried/carry-over: o carry AVANÇA `order_date` na MESMA linha — as colunas novas
  viajam junto; nada a fazer.
- Rebuildar server E front antes de validar (binário velho engana — gotcha recorrente).

## Fora de escopo (explícito)

- Renomear `id` de def (imutável por design; a Opção A — id derivado do Job Name —
  já foi entregue em 2026-07-17, ver changelog).
- Purga de instances de def deletada (delete-by-absence fica como está).
