<div align="center">
  <img src="public/favicon-512.png" width="96" alt="Regente" />
  <h1>Regente — Web (frontend)</h1>
  <p><strong>The frontend of Regente, a Git-native workflow orchestrator with enterprise-class batch semantics.</strong></p>
</div>

> 📦 This is the **`app/`** folder of the monorepo. The project overview, the architecture and
> how to install the **server** and the **agent** live in the **[root README](../README.md)**.

The frontend (React + TypeScript + Vite) is the HTTP/WebSocket client of `regente-server`:
Monitoring (what is running today) and Design (a drag-and-drop canvas of the definitions, with
Publish to Git). The UX is the one a batch operator knows (folders, hold/rerun/force, find &
update).

---

## Concept

Two separate worlds, classic enterprise style:

- **Monitoring** — what is running today; the result of the *Daily*. Consumption only:
  hold / release / cancel / set-ok / rerun / force order, per-instance audit, SLA.
- **Design** — where the definitions are edited. You open one or more *folders* (an ephemeral Git
  clone per session), edit on the drag-and-drop canvas and hit **Publish** (the only write path
  to GitHub).

**The Daily** runs once a day: it reads Git, decides what runs today (schedule + calendars +
dependencies + conditions) and materializes **immutable** *instances* — a change published in
Design during the day only takes effect on the next daily, or through a manual Force Order.

## Stack

| Layer | Technology |
|---|---|
| Frontend (this folder) | React + TypeScript + Vite, [@xyflow/react](https://reactflow.dev) (canvas), lucide icons |
| Backend | Go (`regente-server`) — GitOps + SQLite/Postgres, WebSocket hub |
| Executor | Go (`regente-agent`) — outbound connection (WS · HTTP long-poll · SSE) |
| Source of truth | A GitHub repository (YAML at `definitions/<folder>/<id>.yaml`) |

## Running the frontend

Requirements: Node.js: `^20.19.0 || ^22.13.0 || >=24` and a running `regente-server`.
Node 24 is used by CI. The range includes ESLint's stricter Node 22 minimum,
not just Vite's build engine. The locked dependencies are checked in CI.

```bash
npm ci
cp .env.example .env        # set VITE_REGENTE_SERVER_URL
npm run dev                 # http://localhost:5173
```

Environment variables (`.env`):

```bash
VITE_REGENTE_SERVER_URL=http://localhost:8080   # empty = local mode (localStorage)
```

Default dev login: `admin` / `admin`.

`VITE_REGENTE_SERVER_URL=@origin` means **same-origin**: the Go server serves the SPA on its own
port (`-spa-dir`), so the UI resolves `window.location.origin` at runtime. This is how the
single-origin deployment and the tunnelled demo work — the URL can change without rebuilding the
frontend.

Build the server-hosted SPA from `app/` (the variable must reach **build**, not
only dependency installation):

```sh
npm ci && VITE_REGENTE_SERVER_URL=@origin npm run build
```

PowerShell equivalent:

```powershell
npm ci
if ($LASTEXITCODE -ne 0) { throw "npm ci failed" }
$previousOrigin = $env:VITE_REGENTE_SERVER_URL
try {
  $env:VITE_REGENTE_SERVER_URL = '@origin'
  npm run build
  if ($LASTEXITCODE -ne 0) { throw "SPA build failed" }
} finally { $env:VITE_REGENTE_SERVER_URL = $previousOrigin }
```

Point the Go server at the resulting `app/dist` with `-spa-dir`. Browser login
uses the server session; do not embed an administrative token in the SPA.
An empty server URL still deliberately selects localStorage mode for offline UI
development; it is not the server-hosted deployment recipe.

## Architecture

```
  ┌──────────────┐    REST + WebSocket    ┌──────────────────┐   git push/pull   ┌──────────┐
  │  Frontend     │ ─────────────────────▶ │  regente-server   │ ◀───────────────▶ │  GitHub   │
  │  (this folder)│ ◀───────────────────── │  (Go, SQLite/PG)  │   (source of      │  (YAML)   │
  └──────────────┘    instance.changed     └──────────────────┘    truth)          └──────────┘
                                                    ▲
                                                    │ WebSocket (the agent dials out)
                                                    │ dispatch ▼   ▲ result
                                            ┌──────────────────┐
                                            │  regente-agent    │  runs COMMAND/SCRIPT/HTTP
                                            │  (your PC / EC2)  │  on Windows or Linux
                                            └──────────────────┘
```

In development:

```bash
# server (SQLite; offline unless you explicitly configure -git-source)
cd ../server && go run . -addr 127.0.0.1:8080 -api-token=
```

Log in locally, then in **Settings > Agents** provision ID `my-pc`, empty
environment and capabilities `COMMAND,SCRIPT,HTTP`. Transfer the one-time
machine secret to the agent's protected `REGENTE_TOKEN` environment (do not put it
in command history). In another terminal, from `agent/`:

```sh
go run . -server ws://localhost:8080/ws/agent -id my-pc -env= -caps COMMAND,SCRIPT,HTTP
```

Do not reuse a login token or the server administrative bearer. Verify authenticated
presence in Settings > Agents, then run a synthetic COMMAND pinned to `my-pc`.
See [identity lifecycle](../docs/agent-identity.md) and the
[local demo smoke](../deploy/demo/README.md) for automated provisioning and validation.

## Layout

```
src/
├── v2/                  # the current UI (Monitoring, Design, drawers, dialogs)
│   ├── V2Preview.tsx    # main shell (topbar, canvas, modes)
│   ├── JobConfigDrawer  # job editing (General/Schedule/Calendars/Action/Conditions)
│   ├── ScheduleEditor   # visual enterprise-style scheduler
│   ├── AlertsPanel.tsx  # alerts screen (events + rules + channels)
│   └── ...
├── lib/                 # API clients + model + adapters
│   ├── server-client.ts # REST + WS
│   ├── git-api.ts       # status, token, cleanup, deep links
│   ├── agents-api.ts    # online agents
│   ├── alerts-api.ts    # alerts (dual-mode server/local facade)
│   └── adapters/        # ports & adapters (storage/scheduler/executor)
└── main.tsx
```

## Checks

```bash
npm run build     # tsc -b && vite build
npm run lint      # eslint — a CI gate, it must stay at zero
```

---

<sub>A personal portfolio project, independently designed and built. It is not affiliated with,
endorsed by, or derived from any commercial orchestration product.</sub>
