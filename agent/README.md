# regente-agent

Regente's local executor. Run **one agent on every machine** that should execute jobs (your
laptop, an on-prem server, a VM/EC2, and so on).

It connects to `regente-server` with an **outbound** connection — no ports need to be opened on
the agent's machine, and it crosses NAT and firewalls. Same runner model as GitHub Actions,
GitLab or a classic enterprise scheduler's agent. It runs jobs locally, returns the result, and **streams
stdout/stderr in real time** (visible in the instance detail).

## Executors

| jobType | What it does | Params (actionConfig) |
|---|---|---|
| `COMMAND` | A command in the OS shell (`powershell -Command` on Windows, `sh -c` on Linux) | `command`, `cwd?` |
| `SCRIPT` | Runs a script; the interpreter comes from the extension (`.ps1`/`.bat`/`.sh`) | `scriptPath`, `args?`, `cwd?` |
| `HTTP` | A REST call with status validation | `method`, `url`, `headers?`, `body?`, `expectStatus?` |
| `DATABASE` | SQL against Postgres/MySQL/SQLite (pure-Go drivers, no client on the host) | `driver`, `dsn`, `sql`, `maxRows?` |
| `FILE_WATCH` | Waits for a file to arrive (and settle) on the agent's host | `path`, `intervalSec?`, `stableSec?` |
| `FILE_TRANSFER` | **Native MFT**: moves files between local, SFTP and S3 — globbing at the source, atomic writes, SHA-256 checksum (alias `MFT`) | `src`, `dst`, `checksum?`, `deleteSource?`, `overwrite?`, … |
| `LAMBDA` | Invokes an AWS Lambda function (SigV4 on the stdlib, no SDK) | `function`, `region?`, `payload?` |
| `BATCH` · `GLUE` · `STEP_FUNCTION` | AWS Batch job, Glue job run, Step Functions execution | see the API reference |
| `GCP_RUN` | Triggers a Cloud Run Job (Run Admin API v2) | `project`, `region`, `job` |
| `WASM` | Runs a sandboxed WASI WebAssembly module through [wazero](https://wazero.dev) (pure Go, no CGO) | `wasmPath` \| `wasmUrl`, `args?`, `stdin?` |
| `K8S_JOB` | Creates a **Kubernetes Job** through the REST API and waits for it | `image`, `command?`, `namespace?`, `apiServer?`, `token?` |

> `SSH` (an agentless remote command) does **not** use the agent — it runs on the server itself.
> `K8S_JOB` needs an agent that announces `-caps K8S` (with access to the cluster API).

## Build

```bash
go build -o regente-agent .      # Linux/macOS
go build -o regente-agent.exe .  # Windows
```

## Run (foreground)

```bash
./regente-agent \
  -server ws://YOUR-SERVER:8080/ws/agent \
  -token  rgta_...        # Settings → Agents → Create token \
  -id     my-host \
  -caps   COMMAND,SCRIPT,HTTP
```

Token: create **one token per agent** in the UI (Settings → Agents → Create token). The legacy
`dev-token` still works in dev. Without a valid token the handshake is refused.

**Transport** (`-transport`): `ws` (WebSocket, the default), `http` (long-poll) or `sse`
(Server-Sent Events, immediate push). The last two are serverless-friendly — still outbound, but
they let the control plane be stateless and scale to zero. See
[architecture-future.md](../docs/architecture-future.md).

## Install as a service

The friendly installers download the binary from the latest release and register the agent as a
service (**systemd** · **launchd** · **Scheduled Task**) — it starts at boot, restarts if it
crashes, and runs with nobody logged in. **No Docker, Go or runtime needed.** They prompt for the
server and the token if you do not pass them.

```bash
# Linux or macOS
curl -fsSL https://github.com/Dr0nj/regente/releases/latest/download/install-agent.sh -o install-agent.sh
sudo bash install-agent.sh
# fleet / unattended:
sudo SERVER=wss://YOUR-DOMAIN/ws/agent TOKEN=rgta_xxx bash install-agent.sh
```

```powershell
# Windows (PowerShell as Administrator)
irm https://github.com/Dr0nj/regente/releases/latest/download/install-agent-windows.ps1 | iex
# fleet / unattended: download the .ps1 and run it with  -Server wss://... -Token rgta_xxx
```

Building from source instead? Use [`deploy/install-linux.sh`](deploy/install-linux.sh) or
[`deploy/install-windows.ps1`](deploy/install-windows.ps1).

## Dispatch: how the server picks an agent

- A job with a **specific agent** (the "Agent (where it runs)" field in the job drawer) goes
  straight to it. The pin is strict: if that agent is down, the job waits in WAIT AGENT until it
  comes back — it never migrates on its own.
- Otherwise the server picks an online agent whose **capability** matches the jobType
  (`PickAgent`). That is why `-caps` must list the jobTypes the agent accepts.
- **Environment** (the job's `environment` versus the agent's `-env` flag): a side with no label
  is a wildcard; when both have labels they must match (case-insensitive). A `prod` job
  **never** lands on a `dev` agent — not even pinned (it stays in WAIT AGENT with the reason in
  the Explain).

## WebSocket protocol

```jsonc
// server → agent
{ "event":"dispatch", "instanceId":"...", "jobType":"COMMAND", "params":{...}, "timeout":300 }
// server → agent (kill a running job)
{ "event":"cancel", "instanceId":"..." }
// agent → server (streaming while it runs)
{ "event":"output", "instanceId":"...", "chunk":"a line of stdout/stderr" }
// agent → server (final)
{ "event":"result", "instanceId":"...", "exitCode":0, "output":"the full output" }
// agent → server (every 30s)
{ "event":"heartbeat" }
```
