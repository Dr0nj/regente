# `regente` — the developer-experience CLI

This is where the classic enterprise orchestrators lose badly: the **define → test → run locally → promote → operate**
cycle becomes a command line plus Git, with no proprietary console in the middle.

```
go build -o regente ./cmd/regente
```

## `regente test <job.yaml | workspace-dir>`

Validates and **simulates** without a server. The pipeline:

1. **strict** parsing (an unknown field is an error — it catches typos before runtime);
2. structural validation (id/label/team, actionConfig per jobType — the same as the API's save);
3. the graph: an upstream pointing at a job that does not exist · a dependency **cycle**;
4. **policy as code**: the workspace's `policies.yaml`, if there is one;
5. a **daily simulation** for `-date` using the SAME engine as the server
   (`DryRun`/`IsScheduledOn`): who RUNS, who WAITS, who NEVER fires.

Exit `0` = passed (warnings are fine) · `1` = failed → use it directly in the workspace repo's
CI.

```
regente test ./regente-workspace -date 2026-07-08        # the whole workspace
regente test job.yaml -json                              # JSON output for CI
```

## `regente dev [daily]`

A whole Regente, **disposable**, on a local port: a temporary SQLite (state dies with the
process), demo mode (no agent — jobs mock-finish OK), a local workspace (no Git, no push, no
network), the daily materialized at boot plus the internal ticker.

```
regente dev daily -workspace ./regente-workspace -date 2026-07-08 -addr :8686
```

## `regente promote -from <branch> -to <branch>`

**Git-native** multi-environment promotion: environments are branches of the workspace repo.
Promoting means the snapshot of the promotable paths from the source (definitions/, calendars/,
**policies.yaml** — code AND policy together) **replaces** the destination (add/update/**delete**,
not a merge). It produces a reviewable commit on the destination branch; that environment's
server picks it up through the normal GitOps flow.

```
regente promote -repo https://github.com/org/regente-workspace.git -from dev -to staging
regente promote -from dev -to main -folders finance,pix        # partial promotion
regente promote -from dev -to main -dry-run                    # just the diff
```

> Flags can come in any order relative to the positional argument
> (`regente test ws -json` == `regente test -json ws`).

## `regente ops <subcommand>`

Operates a **live server**, built entirely on the Go SDK (`pkg/client`) — the CLI never speaks
HTTP directly, so any integration can import the same package.

```
regente ops instances [-date D] [-status NOTOK,WAITING] [-folder F] [-late] [-group status] [-json]
regente ops action <hold|release|cancel|rerun|set-ok|confirm> <instanceId>
regente ops force <definitionId>
regente ops ingest -source ci -id build-123 -condition data-ok
regente ops daily [-report] [-date D] [-json]
regente ops archives [list | get <file> -o output.ndjson]
regente ops jobtypes [-json]
```

Connection: `-server`/`-token`, or the `REGENTE_SERVER`/`REGENTE_TOKEN` environment variables.
The surface is the **curated integration one** (composed query, lifecycle, ingest, daily,
archives, catalog) — the same list the OpenAPI contract documents.

### Go SDK (`pkg/client`)

```go
import "github.com/Dr0nj/regente-server/pkg/client"

cli := client.New("http://localhost:8080", os.Getenv("REGENTE_TOKEN"))
res, _ := cli.QueryInstances(client.Query{Statuses: []string{"NOTOK"}})
_ = cli.Action(res.Items[0].ID, "rerun")
```
