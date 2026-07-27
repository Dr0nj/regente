# importctm — a Control-M → Regente importer

Reads a Control-M export XML (`DEFTABLE`/`FOLDER`/`SMART_FOLDER`/`JOB`, the format produced by
`ctm export`/forecast) and generates a **local Regente workspace** for you to review:

```
importctm -in export.xml -out ./workspace [-dry-run] [-folder-filter FIN]
```

Output:

- `definitions/<folder>/<job>.yaml` — in the SAME YAML dialect as the server's `FileStore`;
- `calendars/<name>.yaml` — a stub (Mon–Fri) for every referenced calendar, with a
  `# TODO-import` note to fill in the holidays;
- `import-report.md` — N jobs **ok** · N **partial** (with `# TODO-import` in the YAML) · N
  **skipped** and why, plus warnings about the attributes that were deliberately ignored.

**The importer NEVER pushes** — the files are local; review the `# TODO-import` notes and commit
them yourself into the workspace repo (`-git-source`). `-dry-run` only prints the report and
writes nothing.

## Mappings (v1)

| Control-M | Regente | Notes |
|---|---|---|
| `FOLDER`/`SMART_FOLDER` + `PARENT_FOLDER` | `team` (the folder) | The job's `PARENT_FOLDER` wins; the fallback is `FOLDER_NAME`. |
| `JOBNAME` | `id` | Slugified (lowercase, `[a-z0-9_-]`); a collision gets a `-2`, `-3`… suffix. |
| `DESCRIPTION` | `label` | Empty → falls back to `JOBNAME`. |
| `TASKTYPE` Job/Command | `jobType: COMMAND` | `CMDLINE` → `params.command` (empty → a TODO). |
| `TASKTYPE` Dummy | `COMMAND` with `dryRun: true` | A job that "runs without doing anything" (Regente's 👻). |
| `TASKTYPE` FileWatcher | `jobType: FILE_WATCH` | `FILE_NAME`/`FILE_PATH` → `params.path` (empty → a TODO). |
| other `TASKTYPE` values | **skipped** | Listed in the report with the reason. |
| `TIMEFROM` (`HHMM`) | `schedule.runAt` | `TIMETO` → `schedule.windowTo`. |
| `WEEKDAYS` (`1,2,5` or `MON,TUE`) | `frequency: weekly` + `daysOfWeek` | `0`/`7` = Sunday; an unknown token → a TODO. |
| `DAYS` (`1,15,L`) | `frequency: monthly` + `daysOfMonth` | `L` (last day) → `-1`; `DAYS`+`WEEKDAYS` together (AND/OR) → a TODO. |
| `DAYS`/`WEEKDAYS` empty or `ALL` | `frequency: daily` | |
| `DAYSCAL` / `WEEKSCAL` | `calendars: [{name, mode: include}]` | Generates the stub in `calendars/` (holidays = a TODO). |
| `CONFCAL` + `SHIFT` | calendar include + `schedule.shift` | `>` → `next-businessday` · `<` → `prev-businessday`; anything else → a TODO. |
| `INCOND` | `upstream` OR `conditionsIn` | A condition whose `OUTCOND +` has **exactly one emitting job** in the export becomes an `upstream {from, on-success}` edge; zero emitters (an external system) or two or more → `conditionsIn` with the SAME name. `ODATE≠ODAT` and `AND_OR=O` → a TODO. |
| `OUTCOND SIGN=+` | `conditionsOutAdd` | Omitted when the condition became an edge (one emitter — the dependency is already in the consumer's upstream). |
| `OUTCOND SIGN=-` | `conditionsOutRemove` | |
| `SHOUT WHEN=OK/NOTOK` | `actions: [{on: result, do: notify}]` | `URGENCY`: `V`→critical · `U`→warning · `R`/empty→info. `DEST` is ignored with a warning (notify channels are the alerting sinks). Other `WHEN` values (LATE…) → a TODO. |
| `MAXRERUN` | `retries` | |
| `CYCLIC` + `INTERVAL` | `schedule.cyclic` + `intervalMin` | `00030M`→30 · `2H`→120 · `1D`→1440 · `45`→45 minutes; unparseable → a TODO. |
| `VARIABLE %%NAME` | `variables.NAME` | The value is kept as-is (Regente's own `%%` tokens cover ODATE and friends). |

## Ignored WITH A WARNING (no 1:1 equivalent in v1)

`SUB_APPLICATION` · `APPLICATION` · `DATACENTER` · `RUN_AS` · `NODEID` · `CREATED_BY` ·
`AUTHOR` · `MEMNAME` · `MEMLIB` — they show up in the report's "Warnings" section. `NODEID` and
`RUN_AS` are covered in Regente by agents and capabilities (the job's `agentId`) — the routing
decision is left to the reviewer.

## Everything else

Any **other attribute** on a `JOB` becomes a `# TODO-import: attribute X="v" not mapped` line at
the end of the YAML **and** shows up in the report's Pending column — nothing is lost silently.
