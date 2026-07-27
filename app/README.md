<div align="center">
  <img src="public/favicon-512.png" width="96" alt="Regente" />
  <h1>Regente — Web (frontend)</h1>
  <p><strong>The frontend of Regente, a Git-native workflow orchestrator inspired by Control-M.</strong></p>
</div>

> 📦 This is the **`app/`** folder of the monorepo. The project overview, the architecture and
> how to install the **server** and the **agent** live in the **[root README](../README.md)**.

The frontend (React + TypeScript + Vite) is the HTTP/WebSocket client of `regente-server`:
Monitoring (what is running today) and Design (a drag-and-drop canvas of the definitions, with
Publish to Git). The UX is the one a Control-M operator knows (folders, hold/rerun/force, find &
update).

---

## Concept

Two separate worlds, Control-M style:

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

Requirements: Node 18+ and a running `regente-server` (see below).

```bash
npm install
cp .env.example .env        # set VITE_REGENTE_SERVER_URL
npm run dev                 # http://localhost:5173
```

Environment variables (`.env`):

```bash
VITE_REGENTE_SERVER_URL=http://localhost:8080   # empty = local mode (localStorage)
VITE_REGENTE_TOKEN=dev-token
```

Default dev login: `admin` / `admin`.

`VITE_REGENTE_SERVER_URL=@origin` means **same-origin**: the Go server serves the SPA on its own
port (`-spa-dir`), so the UI resolves `window.location.origin` at runtime. This is how the
single-origin deployment and the tunnelled demo work — the URL can change without rebuilding the
frontend.

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
# server (GitOps + SQLite)
cd ../server && go run . -api-token dev-token

# executor agent (on the machine where the commands should run)
cd ../agent  && go run . -server ws://localhost:8080/ws/agent -token dev-token \
                         -id my-pc -caps COMMAND,SCRIPT,HTTP
```

## Layout

```
src/
├── v2/                  # the current UI (Monitoring, Design, drawers, dialogs)
│   ├── V2Preview.tsx    # main shell (topbar, canvas, modes)
│   ├── JobConfigDrawer  # job editing (General/Schedule/Calendars/Action/Conditions)
│   ├── ScheduleEditor   # visual Control-M style scheduler
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

<sub>A personal portfolio project. The UI is inspired by operating Control-M; it has no
relationship with BMC.</sub>
