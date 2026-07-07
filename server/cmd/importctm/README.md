# importctm — importador Control-M → Regente (E6)

Lê o XML de export do Control-M (`DEFTABLE`/`FOLDER`/`SMART_FOLDER`/`JOB`, formato do
`ctm export`/forecast) e gera um **workspace Regente local** para revisão:

```
importctm -in export.xml -out ./workspace [-dry-run] [-folder-filter FIN]
```

Saída:

- `definitions/<folder>/<job>.yaml` — no MESMO dialeto YAML do `FileStore` do server;
- `calendars/<name>.yaml` — stub (seg–sex) de cada calendar referenciado, com
  `# TODO-import` para preencher feriados;
- `import-report.md` — N jobs **ok** · N **parciais** (com `# TODO-import` no YAML) ·
  N **pulados** e por quê, mais os avisos de atributos ignorados de propósito.

**O importador NUNCA faz push** — os arquivos são locais; revise os `# TODO-import`
e commite você mesmo no repo de workspace (`-git-source`). `-dry-run` só imprime o
relatório e não escreve nada.

## Mapeamentos v1

| Control-M | Regente | Observações |
|---|---|---|
| `FOLDER`/`SMART_FOLDER` + `PARENT_FOLDER` | `team` (folder) | `PARENT_FOLDER` do job vence; fallback = `FOLDER_NAME`. |
| `JOBNAME` | `id` | slug (minúsculo, `[a-z0-9_-]`); colisão ganha sufixo `-2`, `-3`… |
| `DESCRIPTION` | `label` | vazio → usa o `JOBNAME`. |
| `TASKTYPE` Job/Command | `jobType: COMMAND` | `CMDLINE` → `params.command` (vazio → TODO). |
| `TASKTYPE` Dummy | `COMMAND` com `dryRun: true` | job que "roda sem fazer nada" (👻 do Regente). |
| `TASKTYPE` FileWatcher | `jobType: FILE_WATCH` | `FILE_NAME`/`FILE_PATH` → `params.path` (vazio → TODO). |
| outros `TASKTYPE` | **pulado** | listado no relatório com o motivo. |
| `TIMEFROM` (`HHMM`) | `schedule.runAt` | `TIMETO` → `schedule.windowTo` (bônus natural). |
| `WEEKDAYS` (`1,2,5` ou `MON,TUE`) | `frequency: weekly` + `daysOfWeek` | `0`/`7` = dom; token estranho → TODO. |
| `DAYS` (`1,15,L`) | `frequency: monthly` + `daysOfMonth` | `L` (último dia) → `-1`; `DAYS`+`WEEKDAYS` juntos (AND/OR) → TODO. |
| `DAYS`/`WEEKDAYS` vazios ou `ALL` | `frequency: daily` | |
| `DAYSCAL` / `WEEKSCAL` | `calendars: [{name, mode: include}]` | gera o stub em `calendars/` (feriados = TODO). |
| `CONFCAL` + `SHIFT` | calendar include + `schedule.shift` | `>` → `next-businessday` · `<` → `prev-businessday`; outros → TODO. |
| `INCOND` | `upstream` OU `conditionsIn` | cond cujo `OUTCOND +` tem **exatamente 1 job emissor** no export vira aresta `upstream {from, on-success}`; 0 emissores (sistema externo) ou 2+ → `conditionsIn` com o MESMO nome (F16). `ODATE≠ODAT` e `AND_OR=O` → TODO. |
| `OUTCOND SIGN=+` | `conditionsOutAdd` | omitido quando a cond virou aresta (1 emissor — a dependência já está no upstream do consumidor). |
| `OUTCOND SIGN=-` | `conditionsOutRemove` | |
| `SHOUT WHEN=OK/NOTOK` | `actions: [{on: result, do: notify}]` | `URGENCY`: `V`→critical · `U`→warning · `R`/vazio→info. `DEST` é ignorado com aviso (os canais do notify são os sinks do alerting). Outros `WHEN` (LATE…) → TODO. |
| `MAXRERUN` | `retries` | |
| `CYCLIC` + `INTERVAL` | `schedule.cyclic` + `intervalMin` | `00030M`→30 · `2H`→120 · `1D`→1440 · `45`→45 min; não parseou → TODO. |
| `VARIABLE %%NOME` | `variables.NOME` | valor mantido como está (os tokens `%%` do Regente cobrem ODATE etc.). |

## Ignorados COM AVISO (sem equivalente 1:1 no v1)

`SUB_APPLICATION` · `APPLICATION` · `DATACENTER` · `RUN_AS` · `NODEID` ·
`CREATED_BY` · `AUTHOR` · `MEMNAME` · `MEMLIB` — aparecem na seção "Avisos" do
relatório. `NODEID`/`RUN_AS` são cobertos no Regente por agentes/capabilities
(`agentId` do job) — decisão de roteamento fica pro revisor.

## Tudo o mais

Qualquer **outro atributo** do `JOB` vira `# TODO-import: atributo X="v" não mapeado`
no fim do YAML **e** entra na coluna Pendências do relatório — nada se perde em
silêncio.
