# 🤖 Regente MCP — the agent-native layer

`regente-mcp` is an [MCP (Model Context Protocol)](https://modelcontextprotocol.io) server that
exposes Regente's **deterministic differentiators** as _tools_ for an agent (Claude Desktop and
friends). The idea: you **operate Regente by talking to it** — *"what failed in payments today,
and why?"* — and the agent calls the tools, which return the truth the engine already computed
(the LLM **narrates**; it does not invent).

> Architecture: it is a **facade** over the REST API — it never touches the server core.
> Pure-Go stdlib, JSON-RPC 2.0 over stdio (the transport Claude Desktop uses).

## Tools

Read-only (always available) — **11**:

| Tool | What it does | Endpoint |
|---|---|---|
| `daily_summary` | overview of the day (total · by status · by folder) | `/api/instances/summary` |
| `forecast` | schedule forecast (dry run); `days>1` forecasts a window of a week or more | `/api/forecast` · `/api/forecast/range` |
| `list_instances` | finds instances by folder/status/search (to get the `instanceId`) | `/api/instances` |
| `explain_job` | why a job did (or did not) run — structured gating | `/api/instances/{id}/explain` |
| `blast_radius` | impact of cancelling/holding a job (downstream · SLA) | `/api/instances/{id}/blast-radius` |
| `job_neighborhood` | local graph (ancestors/descendants up to `radius` hops) | `/api/instances/{id}/neighborhood` |
| `root_cause` | root cause of a failure or block (walks up the chain of failed upstreams) | `/api/instances/{id}/rca` |
| `diff_daily` | what changed between two dailies | `/api/daily/diff` |
| `dry_run` | simulates a future daily (runs/waits/never) | `/api/daily/dryrun` |
| `event_log` | the day's event feed (cross-instance), filterable | `/api/events` |
| `query` | a natural-language question (EN/PT) → a deterministic answer | `/api/query` |

Write (only with `-allow-writes`; the MCP client still asks for approval per call) — **11**, all
marked **destructiveHint**:

| Tool | What it does | Endpoint |
|---|---|---|
| `hold_job` | holds a job in any status except RUNNING → HELD, freezing the original status (classic Hold) | `POST /api/instances/{id}/hold` |
| `release_job` | releases a HELD job back to the **original** status frozen by the hold (Release) | `POST /api/instances/{id}/release` |
| `cancel_job` | cancels a job for the day (→ CANCELLED) | `POST /api/instances/{id}/cancel` |
| `confirm_job` | confirms a job sitting at the WAIT_CONFIRM gate (classic Confirm) | `POST /api/instances/{id}/confirm` |
| `rerun_job` | re-runs a job (→ WAITING) | `POST /api/instances/{id}/rerun` |
| `set_ok` | marks NOTOK/CANCELLED as OK and unblocks the successors | `POST /api/instances/{id}/set-ok` |
| `force_order` | orders and runs a **definition** right now, outside the schedule | `POST /api/definitions/{id}/force` |
| `pause_folder` | pauses a whole workflow: every job of the folder for the day (any status except RUNNING, carry-over included) → HELD, state preserved | `POST /api/folders/{name}/pause` |
| `resume_folder` | resumes the workflow: everything held by the pause goes back to its **original** status | `POST /api/folders/{name}/resume` |
| `bulk_action` | one action across N instances — hold/release/cancel/rerun/set-ok/confirm/delete (transactional per item, max 500) | `POST /api/bulk/instances` |
| `ingest_event` | sets conditions and/or forces a job through an external event (idempotent) | `POST /api/events/ingest` |

## Build & run

```bash
cd server && go build -o regente-mcp ./cmd/mcp
REGENTE_URL=http://localhost:8080 REGENTE_TOKEN=<token> ./regente-mcp
# writes (hold/release/cancel/confirm/rerun/set-ok · force_order ·
# pause/resume_folder · bulk_action · ingest_event), optional:
./regente-mcp -allow-writes
```

## Claude Desktop

In `claude_desktop_config.json` (Settings → Developer → Edit Config):

```json
{
  "mcpServers": {
    "regente": {
      "command": "/path/to/regente-mcp",
      "env": {
        "REGENTE_URL": "http://localhost:8080",
        "REGENTE_TOKEN": "your-token"
      }
    }
  }
}
```

Restart Claude Desktop. Then just ask: *"using regente, give me today's daily summary"*, *"why
didn't the job closing-2026-06-24 run?"*, *"if I cancel PIX_SEND right now, what's the impact?"*,
*"what changed in today's daily versus yesterday's?"*, *"forecast next week"*. With
`-allow-writes` the agent can also **act** (always with approval): *"hold the PIX folder until I
release it"*, *"mark the closing job as OK and unblock the successors"*, *"force etl-sales now"*,
*"the file arrived: set the condition SALES_FILE_OK"*.

## Security posture

- **Read-only by default.** Writes require `-allow-writes` on the server **and** human approval
  in the MCP client on every call (two locks).
- **Deterministic.** The tools return what the scheduler already computed (gating, the
  dependency graph, frozen snapshots). The LLM narrates — it never decides scheduling.
- The token is the same API Bearer token; give it only the role (RBAC) the use case needs.
