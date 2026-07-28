<div align="center">
  <img src="app/public/logo-r.png" width="92" alt="Regente" />
  <h1>Regente</h1>
  <p><strong>A Git-native workflow orchestrator, inspired by Control-M.</strong></p>
  <p>
    <a href="#-what-it-is">What it is</a> ·
    <a href="#-documentation">Docs</a> ·
    <a href="#-try-it-in-5-minutes">Try it</a> ·
    <a href="#-installation">Installation</a> ·
    <a href="#-what-it-can-do">Capabilities</a> ·
    <a href="#-architecture">Architecture</a> ·
    <a href="#-development">Development</a>
  </p>
</div>

---

## 🧭 What it is

Regente runs your **scheduled jobs** — the scripts, commands, transfers and API calls that have
to happen every day, in the right order, at the right time, on the right machine.

If you have never used a tool like this, here is the idea in four sentences:

1. You **draw** your jobs on a canvas and connect them with arrows ("run B after A succeeds").
2. Every box you draw is stored as a **YAML file in a Git repository** — that repository is the
   single source of truth, so every change is reviewable and revertible.
3. Once a day Regente reads that repository and decides **what runs today**, creating one
   *instance* per job. That day's picture is then frozen: editing a job in the afternoon does not
   rewrite the morning's run.
4. The jobs actually execute on **your** machines, through a small program called an **agent**
   that you install there.

Two screens, kept deliberately apart:

- **Monitoring** — what is running *today*. You watch it and act on it: hold, release, cancel
  (which kills the running process), mark as OK, re-run, force a job to run now. You cannot edit
  job definitions here.
- **Design** — where job definitions are edited. You open a folder, edit on the canvas, and press
  **Publish**, which is the only path that writes to GitHub. Drafts are never lost: refreshing
  the page or switching screens resumes where you left off.

This repository is the **monorepo** for the whole platform:

| Folder | Component | Stack |
|---|---|---|
| [`server/`](server) | **regente-server** — the daemon (REST API + WebSocket hub, scheduler, GitOps) | Go |
| [`agent/`](agent) | **regente-agent** — the executor that runs on your machines | Go |
| [`app/`](app) | **regente-web** — the frontend (Monitoring, Design canvas) | React + TypeScript + Vite |

> The jobs themselves (the *definitions*, as YAML) live in a **separate** GitOps workspace
> repository. This repo holds only the platform **code**.

> 🌐 **Language:** the **product speaks English** — the UI, server messages, the CLI, agent
> output, the OpenAPI contract and the installation scripts. The **planning documents and the
> code comments in this repo are in Portuguese** (they are read by whoever develops it, not by
> whoever operates it).

---

## 📚 Documentation

**Nothing to install, nothing to start — just click:**

| Page | What is in it |
|---|---|
| 📖 [**Documentation**](https://dr0nj.github.io/regente/) | This README, the component guides, operations, DR, SLOs, MCP and the conditions spec — all in one navigable place. |
| 🔌 [**API reference**](https://dr0nj.github.io/regente/api.html) | The full OpenAPI contract: every endpoint, parameter, schema and example. Also as [openapi.yaml](https://dr0nj.github.io/regente/openapi.yaml) and [openapi.json](https://dr0nj.github.io/regente/openapi.json) for Postman or a code generator. |

The site is generated from the markdown in this repository and published by
[a workflow](.github/workflows/pages.yml) on every push to `main`. It is also committed to
[`docs/site/`](docs/site), so **it works offline too**: clone or download the repo and open
`docs/site/index.html` in your browser — double-clicking is enough. The pages are
self-contained, with the CSS inlined and the API spec embedded, so there is nothing to fetch.

To regenerate it after editing any markdown:

```bash
cd server && go run ./cmd/docsite -repo .. -out ../docs/site
```

A running server serves the same content live: the docs at `/docs` (with `-docs-dir`) and an
interactive API explorer at `/api-docs`, where you can send real requests with your token.

Deeper documents: [operations](docs/operations.md) · [DR and backup](docs/dr-backup.md) ·
[SLOs](docs/slos.md) · [conditions spec](docs/conditions-events.md) ·
[MCP / AI agents](docs/mcp.md) · [future architecture](docs/architecture-future.md).

---

## 🚀 Try it in 5 minutes

The fastest way to see it working — nothing to configure, nothing left behind. You need
[Go 1.25+](https://go.dev/dl/).

```bash
git clone https://github.com/Dr0nj/regente.git && cd regente/server
go run ./cmd/regente dev daily -addr :8686
```

That starts a complete, **disposable** Regente on <http://localhost:8686>: a temporary database
that dies with the process, no Git and no network, and jobs that finish successfully without a
real agent. Open the URL and log in with `admin` / `admin`.

When you want the real thing, keep reading.

---

## 📦 Installation

### Before you start (5 minutes, do this once)

You need three things. None of them requires knowing Go, Node or Docker.

1. **A Linux machine with systemd and root access** — any VPS (Ubuntu 22.04+, Debian 12+,
   Rocky/Alma 9+) or a VM at home. 1 vCPU and 1 GB of RAM is enough to start. `curl` and `tar`
   must be present (they usually are).
2. **A port you can reach.** The server listens on **8080** by default. On a cloud VM you have to
   open it in **two** places: the machine's firewall *and* the provider's security group /
   firewall rules. If you plan to put it behind a domain with HTTPS, read
   [`deploy/vps/`](deploy/vps/README.md) instead — there the port stays closed and nginx handles
   443.
3. **An empty GitHub repository for your workspace** — this is where your job definitions will
   live. Create it on GitHub ("New repository", **no** README, **no** .gitignore — leave it
   completely empty; private is fine) and keep two things at hand:
   - its name as `owner/name`, e.g. `acme/regente-workspace`;
   - a **Personal Access Token** with write access to it. Fine-grained: *Repository access →
     only that repo*, *Permissions → Contents: Read and write*. Classic: the `repo` scope.
     ([Create one here.](https://github.com/settings/tokens))

> **The workspace repository is yours and is different in every installation.** Regente does not
> ship a default one. On the first start the server writes the initial content into your empty
> repository (`definitions/`, `.gitignore`, a README) and pushes it — you do not have to prepare
> anything inside it. The clone lives in `/var/lib/regente/workspace`, so it survives service
> restarts, machine reboots and binary upgrades; and because the repository is the source of
> truth, a rebuilt machine recovers everything by cloning it again.
>
> **Already have a workspace repository?** Point the installation at it and everything comes
> back: the folder structure, the job definitions, their conditions and the per-folder layout.
> That is the ordinary path for moving to a new machine or reinstalling — the server clones the
> repository and the next daily materializes those jobs. Nothing has to be prepared by hand, and
> the branch does not have to be `main` (if the configured branch does not exist, the server
> follows the repository's default and says so in the log). What does **not** come from the
> repository is the runtime state — users, agent tokens and the run history live in the database;
> to carry those over, back up and restore it ([DR and backup](docs/dr-backup.md)).
>
> You can also skip step 3 entirely: with no workspace repository configured, Regente runs
> **offline** and keeps the definitions on the local disk only. You can point it at a repository
> later — if the disk already has jobs and the repository is still empty, they become its first
> commit. If **both** have content, the server refuses to overwrite either side and tells you the
> two ways out (keep the repository, or publish the disk) — it never picks for you.

### Installing the server

There are three ways to run the server and two ways to run an agent. Pick by what the machine
should do.

**The recommended default is systemd**: the service is supervised (`Restart=always`), so it
starts at boot and comes back on its own after a crash, and its state lives in
`/var/lib/regente`, so it survives reboots and upgrades.

Every option below comes in two flavours:

- **From a release** — `install.sh` downloads a ready-made bundle. **No Go and no Node needed on
  the machine.** This is the easy path.
- **From source** — you build it yourself. Needs Go 1.25+, plus Node 18+ if you want the UI.

> **On Windows?** [`server/deploy/`](server/deploy) and [`agent/deploy/`](agent/deploy) each have
> an `install-windows.ps1` that registers a Scheduled Task (starts at boot, restarts on failure).
> Run PowerShell **as Administrator**.

### Option 1 — server only (headless API)

A control plane **with no UI attached**. Use it when you drive Regente through the API or the
CLI, or when the UI is served from somewhere else.

```bash
# from a release (no toolchain needed):
curl -fsSL https://github.com/Dr0nj/regente/releases/latest/download/install.sh -o regente-install.sh
sudo WITH_UI=0 bash regente-install.sh

# or from source:
cd server && CGO_ENABLED=0 go build -o regente-server . && sudo WITH_UI=0 ./deploy/install-linux.sh
```

### Option 2 — server + UI on one port ⭐ recommended

The UI, the API and the WebSocket share the **same port and origin**, so there is no CORS to
configure and the frontend resolves its own address at runtime — which means the same build works
behind any domain or tunnel.

```bash
# from a release (binary + UI + systemd unit, all ready):
curl -fsSL https://github.com/Dr0nj/regente/releases/latest/download/install.sh -o regente-install.sh
sudo bash regente-install.sh

# or from source (it builds the UI and wires it up on its own):
cd server && CGO_ENABLED=0 go build -o regente-server .
(cd ../app && VITE_REGENTE_SERVER_URL=@origin npm ci && npm run build)
sudo ./deploy/install-linux.sh
```

The installer ends by printing a health check and the next steps. If it says
`Health: FAILED`, the server did not come up — `journalctl -u regente-server -n 50 --no-pager`
says why.

### Option 3 — server + a local agent (single-box lab)

The server **and** an agent on the same machine, so that box also executes jobs. In production
your agents live on the **other** machines; putting them together is a convenience for testing.

```bash
# 1) install the server (option 1 or 2), then:
cd agent && CGO_ENABLED=0 go build -o regente-agent .
sudo SERVER=ws://localhost:8080/ws/agent TOKEN=<token> ID=$(hostname) \
     CAPS=COMMAND,SCRIPT,HTTP ./deploy/install-linux.sh
# create the TOKEN in Settings → Agents (or use REGENTE_TOKEN while developing).
```

### After installing the server (all options)

**1. Configure it.** The guided script asks for the three things that matter — a strong token,
your workspace repository (`owner/name`) and its PAT — and writes them to
`/etc/regente/server.env`:

```bash
sudo regente-configure
```

It offers to restart the service at the end and then tells you whether GitOps actually
connected. Leaving the repository blank is a valid answer: that is offline mode.

**2. Open the port** — on a cloud VM, in the provider's security group *as well*:

```bash
sudo ufw allow 8080/tcp
```

**3. Open `http://<your-host>:8080`** and log in with `admin` / `admin` — it will make you change
the password.

> ⚠️ **`REGENTE_TOKEN` is admin-equivalent** — it bypasses the login entirely. Generate a strong
> value and never leave it as `dev-token` or `change-me`. `regente-configure` generates one for
> you, and the server logs a loud warning at boot if it is still the example value.

**If something is wrong with the workspace repository, the server still starts.** A typo in the
repository name or a PAT without write access does not take the service down: it comes up, the
UI is reachable, and the reason shows up both in the git badge (`⚠ not connected`) and in
`GET /api/git/status` (field `error`). Only the daily is skipped until it connects (the log says
`daily … skipped`). Recovery depends on what was wrong:

- **transient** (GitHub down, network out) — the server retries on its own, nothing to do;
- **missing or invalid PAT** — save it in Settings → GitHub and it applies immediately;
- **wrong repository or branch** — `sudo regente-configure` and let it restart the service.

Useful commands afterwards:

```bash
sudo systemctl restart regente-server     # apply a new binary or config (migrations run at boot)
journalctl -u regente-server -f           # follow the logs
```

### Your first job (2 minutes)

1. **Settings → Agents → create a token**, then install an agent (next section). Or skip it: the
   built-in **SERVER-AGENT** already runs `HTTP` jobs with no agent installed.
2. **Design → New folder**, drop a job on the canvas, pick the type (`COMMAND` for a shell
   command), fill in the command and press **Publish** — that becomes a commit in your workspace
   repository.
3. **Monitoring** shows today's run. Nothing there yet? The daily materializes jobs for the day;
   use **Order Force** on the job to create an order right now.
4. Check your GitHub repository: the YAML of what you just drew is in `definitions/`.

### Agents on the other machines (Linux · macOS · Windows)

An **agent** is what actually runs your jobs, on the machine where you install it. It dials
**out** to the server, so you never open a port on the agent's machine and it works behind NAT
and corporate firewalls.

These installers download the binary from the latest release and register the agent as a proper
service (**systemd** on Linux, **launchd** on macOS, a **Scheduled Task** on Windows): it starts
at boot, restarts if it crashes, and runs with nobody logged in. **No Docker, no Go, no runtime
required.** They prompt for the server address and the token if you do not pass them.

```bash
# Linux or macOS — download it and run it (it asks for the server and the token)
curl -fsSL https://github.com/Dr0nj/regente/releases/latest/download/install-agent.sh -o install-agent.sh
sudo bash install-agent.sh

# a whole fleet, unattended:
sudo SERVER=wss://YOUR-DOMAIN/ws/agent TOKEN=rgta_xxx bash install-agent.sh
```

```powershell
# Windows (PowerShell as Administrator) — it asks for the server and the token
irm https://github.com/Dr0nj/regente/releases/latest/download/install-agent-windows.ps1 | iex

# a whole fleet, unattended: download the .ps1 and run it with  -Server wss://... -Token rgta_xxx
```

Create the **agent token** in Settings → Agents. Your fleet then shows up there, online. Building
from source instead? Use [`agent/deploy/`](agent/deploy).

The server address is the agent's WebSocket endpoint (`ws://host:8080/ws/agent`), but if you
paste the UI address (`http://host:8080`) the installer converts it for you. Both installers
check that the service is really up before they finish, and print the log if it is not.

### When something does not work

| Symptom | What it is | Fix |
|---|---|---|
| `curl … install.sh` gives 404 | that release does not carry the file | check <https://github.com/Dr0nj/regente/releases>; install from source (Option 1/2) meanwhile |
| The page never loads | port closed | `sudo ufw allow 8080/tcp` **and** the provider's security group; test locally first: `curl http://127.0.0.1:8080/health` |
| Installed from source, no UI, only API | the built SPA was not found | `cd app && VITE_REGENTE_SERVER_URL=@origin npm ci && npm run build`, then run the installer again (or pass `SPA_DIR=/full/path/to/app/dist`) |
| Design says sessions are disabled | no workspace repository configured | `sudo regente-configure` and point it at your repository — Design needs GitOps |
| The git badge says `⚠ not connected` | wrong repository, missing PAT, or PAT without write access | PAT: paste it in Settings → GitHub (immediate). Repository/branch: `sudo regente-configure` |
| Nothing runs and the log says `WAIT AGENT` | no agent with that capability is online | install an agent on a machine (previous section) |
| The agent installs but does not appear | wrong URL or token | the log tells which: `journalctl -u regente-agent -n 30` |

### A public link with HTTPS and a real domain

Put the server behind nginx with TLS and a domain. The full systemd recipe — nginx config,
Let's Encrypt, firewall, enterprise hardening (SSO, RBAC, SIEM, mTLS) and a sandboxed agent for
guests — is in [`deploy/vps/`](deploy/vps/README.md).

### Docker · Kubernetes · serverless

A distroless image, a Knative Service with scale-to-zero and an external cron trigger live in
[`deploy/`](deploy/README.md). The same binary runs on Knative, Cloud Run, Fly, App Runner or
Container Apps — you choose the cloud when you deploy, not in the code.

### Showing it to other people (a temporary public demo)

A script that builds everything, serves the UI on one origin, runs the agent in a disposable
Docker container and opens a free public HTTPS tunnel:
[`deploy/demo/`](deploy/demo/README.md).

### Migrating from Control-M

`importctm` reads a Control-M export XML and generates a Regente workspace for review, with an
import report and `# TODO-import` notes wherever a decision is needed. It **never pushes**. See
[`server/cmd/importctm/`](server/cmd/importctm/README.md).

---

## ⚡ What it can do

### Defining and scheduling work

- **Git-native (GitOps)** — Publish becomes a commit or a pull request; changes made on GitHub
  come back into the UI through a webhook plus polling. Deep links go from a job to its YAML and
  from an instance to its commit.
- **Control-M-style scheduling** — frequency (daily / days of the week / days of the month),
  execution windows and cyclic runs. **Calendars** act as include/exclude rules alongside the
  frequency, with a plain-language translation of what each one does and a **real calendar
  preview** (12 mini-months and a year selector; the highlighted days are exactly the days the
  job will run, computed by the backend with the same rule as the daily).
- **Dependencies are CONDITIONS in a single pool** (Control-M global conditions). Drawing A→B on
  the canvas creates the condition `A-TO-B`; typing it by hand lands in exactly the same place.
  A job runs when its input conditions exist in the pool; finishing OK adds its outputs and
  **deletes** the ones it consumed. The Monitoring **Conditions** panel lists the whole pool —
  deleting an entry blocks whoever depends on it, adding one releases them immediately.
  Control-M date references per row: `Odate`, `Prev` and `Stat`.
- **AND/OR logic on the entry** — by default every input condition is required (AND), but a
  toggle groups them, each group with its own operator plus a top-level one, e.g.
  `(C1 AND C2) OR C3` (it runs on the first branch that completes). With a single condition, the
  **"OR run at the scheduled time"** shortcut runs when the condition arrives **or** when the
  start time is reached — whichever comes first.
- **Immutable daily** — instances are frozen at order time. A change published during the day
  only takes effect on the next daily, or through a Force Order.
- **Daily life cycle (Control-M carry-over)** — an order that is still open does not vanish at
  the rollover: RUNNING and HELD always cross; an untreated NOTOK persists one more day (or N,
  through `schedule.keepActive`); OK and CANCELLED close. The order advances its date while
  keeping its id, status and history, and shows where it came from.
- **A schema per job type** — every type declares its parameters (which are required, value
  types, enums, aliases): a typo or a wrong value is flagged **immediately** as you edit, and
  missing required fields are enforced at publish time.
- **Jobs as code** — a YAML editor for the working set right inside Design, with live linting and
  a guide to the dialect.

### Executing it

- **Local executors through an agent** — `COMMAND` (shell), `SCRIPT` (.sh/.bat/.ps1), `HTTP`,
  `DATABASE` (Postgres/MySQL/SQLite through pure-Go drivers, no client installed on the host),
  `FILE_WATCH`, and `FILE_TRANSFER` (native MFT between local, SFTP and S3, with globbing,
  atomic writes and SHA-256 checksums). Every job can target a specific agent or be routed by
  capability, with a token **per agent**. **Strict pinning:** a job pinned to an agent runs only
  there — if that agent goes down, the job waits in WAIT AGENT until it comes back, and never
  migrates on its own.
- **Cloud executors** — AWS Lambda, Batch, Glue and Step Functions; GCP Cloud Run Jobs;
  Kubernetes Jobs. Each one is just a capability, never the foundation.
- **WASM** — sandboxed WASI modules through [wazero](https://wazero.dev) (pure Go, no CGO).
- **SSH, agentless** — a remote command straight from the server, with streamed output.
- **A built-in SERVER-AGENT** — every server ships with a default `HTTP`/`REST` agent, so API
  calls run from the server itself with no external agent to install.
- **Cancel really kills** — cancelling a RUNNING job aborts the process on the agent (the whole
  process tree), and the job ends NOTOK without an automatic retry.
- **Automatic retries** on failure (honouring `retries`), and **cyclic execution** that re-arms
  itself every N minutes inside the window.
- **Confirm** — a job that only runs after an operator releases it (not even Force bypasses it).
- **Resources and quotas** — an exclusive lock, or a pool with a queue, so only N jobs use a
  named resource at a time. The usage tracker is rebuilt after a failover.
- **Multiple environments** — the `environment` field routes execution: an agent started with
  `-env prod` only receives jobs from that environment. A `prod` job never lands on a `dev`
  agent.

### Operating and understanding it

- **Explain — "why didn't this job run?"** For any instance it shows the gating the scheduler
  already computed: the window, dependencies (which upstream or condition), missing conditions,
  an unavailable resource (wanted, in use, capacity). The same evaluator both decides the
  dispatch and explains it, so there is no second implementation to keep in sync.
- **Root cause analysis** — walks up the chain of failed upstreams to the job that actually
  broke.
- **Blast radius** — "if I cancel or hold this job right now, what breaks?" It shows the
  downstream jobs that stop running, the SLAs at risk and the folders affected.
- **Daily diff** — compares two days (today versus yesterday, or an explicit range) and shows
  which jobs were added, removed and changed, field by field.
- **Dry run and forecast** — simulates the daily for any date without materializing anything:
  who runs, who waits (and behind whom) and who never fires, and why. The forecast projects a
  week or more ahead.
- **What-If** — "what if job X is 40 minutes late / takes twice as long / fails?" It projects the
  day, baseline versus scenario, using real durations from history.
- **Statistics per job** — success rate, min/avg/p50/p90/max.
- **Natural-language queries** — ask "how many jobs failed today?" and get a deterministic
  answer, parsed by a rule-based intent parser rather than a model.
- **Output and logs, separated** — the **Output** tab is a live tail of the process's own
  stdout/stderr, kept per attempt (so a retry does not lose the failed attempt's output); the
  **Log** tab is the scheduling journal (ordered → wait → submitted → started → retry →
  finished, plus operator actions).
- **Alerting** — configurable rules evaluated after each run (failure, slowness, retries, success
  rate, consecutive failures), with an alerts screen, real-time toasts, and per-rule routing to
  Slack, a generic webhook, email (SMTP) or PagerDuty. Cooldown is per rule × job, so a burst
  across different jobs never swallows alerts.
- **Bulk actions and find & update** — one action across many instances, and mass edits of
  definitions.
- **Agents screen** — the consolidated fleet (online, plus offline with a last-seen time), a
  detail modal per agent (OS/arch, host, version, uptime, capabilities) and an active ping with
  round-trip latency.

### The platform underneath

- **AI-agent native (MCP)** — an [MCP](https://modelcontextprotocol.io) server exposing **22
  tools**, so you can **operate Regente by talking to it**. Eleven read-only tools are always
  available; eleven write tools sit behind `-allow-writes` **and** per-call approval in the
  client. See [`docs/mcp.md`](docs/mcp.md).
- **Enterprise readiness** — a **Postgres** backend (as well as SQLite), **leader election** for
  scheduler HA, a pluggable **secrets manager**, opt-in **SSO/OIDC**, RBAC with per-folder ACLs,
  and audit forwarding to a SIEM.
- **Portable serverless** — an external time trigger, a pluggable agent transport (WebSocket ·
  HTTP long-poll · SSE · NATS) and a distroless image that scales to zero, with no cloud lock-in.
- **Scale** — validated end to end at Control-M volumes: the write path materializes 1M instances
  in 17s, and the UI was driven live against 1,000,000 jobs without ever downloading a whole day.
- **Observability** — Prometheus metrics at `/metrics`, opt-in OpenTelemetry tracing, plus
  liveness and readiness probes.
- **Themes** — 17 themes (13 dark, 4 light), applied instantly and remembered in the browser.

---

## 🏗 Architecture

```
  ┌──────────────┐    REST + WebSocket    ┌──────────────────┐   git push/pull    ┌──────────┐
  │  app/        | ────────────────────▶ |  server/          │ ◀───────────────▶ │  GitHub   │
  │  (React)     | ◀─────────────────────|  (Go, SQLite/PG)  │   (source of       │  (YAML)   │
  └──────────────┘    instance.changed    └──────────────────┘    truth)          └──────────┘
                                                    ▲
                                                    │ WebSocket (the agent dials out — NAT-friendly)
                                                    │ dispatch ▼   ▲ result
                                            ┌──────────────────┐
                                            │  agent/          |  runs COMMAND/SCRIPT/HTTP/…
                                            │  (your PC / EC2) |  on Windows or Linux
                                            └──────────────────┘
```

- **Local-first:** the agent makes an **outbound** connection to the server (crossing NAT and
  firewalls), receives dispatches, and returns the result and the output stream.
- **Pluggable state store:** SQLite (the default — pure Go, zero infrastructure) or Postgres
  (HA and scale), same codebase, selected with `-db-driver`.
- **HA:** with Postgres, several servers use *leader election* (an advisory lock) — only the
  leader materializes the daily; all of them serve the API.

Component details: [`server/`](server/README.md) · [`agent/`](agent/README.md) ·
[`app/`](app/README.md).

---

## 🛠 Development

Three terminals — server, agent and frontend:

```bash
# 1. server
cd server && go run . -api-token dev-token

# 2. agent (a separate terminal)
cd agent && go run . -server ws://localhost:8080/ws/agent -token dev-token -id my-pc -caps COMMAND,SCRIPT,HTTP

# 3. frontend (a separate terminal)
cd app && npm install && cp .env.example .env && npm run dev    # http://localhost:5173
```

Dev login: `admin` / `admin`.

Before pushing, run the same checks CI runs:

```bash
bash scripts/verify.sh
```

That covers the server (build + vet + test), the agent (build + test) and the app (build). CI
additionally gates on `staticcheck` and `npm run lint`, both of which must stay clean.

If you touched anything on the installation path — the installers, the bundle, the GitOps
bootstrap — run the installation smoke test too. It installs the built artifact on a real
systemd Linux (in a container) and drives the whole operator circuit: install, a wrong
repository (which must not crash-loop), an empty repository, a repository already in use, an
agent, a job that actually runs, and a reboot.

```bash
bash scripts/smoke-install.sh --build
```

**Every push to `main` publishes a release**, and this smoke test is the gate that runs before
it: if the artifact does not install, nothing is published. To skip publishing (a
documentation-only change, for instance) put `[no release]` in the commit **subject** — only the
first line is read, so a commit body may talk about the marker without triggering it. Tag
`vX.Y.Z` by hand when you want a minor or major instead of the next patch.

---

## 🗺 Roadmap

All planning lives in **[`docs/roadmap.md`](docs/roadmap.md)** — the single source of status
(delivered, backlog and changelog). It is an internal working document, written in Portuguese,
and it is deliberately kept out of the published documentation site.

For the **story of the project** — the problem, the architectural bets, Control-M semantics at
the edges, the scale validated at 1M jobs/day and the lessons learned — read the
**[case study](docs/case-study.en.md)**.

---

<div align="center">
<sub>A personal portfolio project. The UX is inspired by operating Control-M; it has no
relationship with BMC.</sub>
</div>
