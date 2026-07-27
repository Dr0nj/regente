# Hosting a Regente demo for other people to try

The goal: a **public https link** where people log in and **create and run real jobs**, without
you exposing your machine or your GitHub beyond what is needed.

## How it works

```
  guests (browser)
        │  https://<something>.trycloudflare.com
        ▼
  Cloudflare Tunnel  ──►  regente-server (your PC, :9091)
                              │   • serves the built SPA   (same port → no CORS)
                              │   • API + WebSocket        (same port)
                              │   • GitOps DIRECT ──► github.com/Dr0nj/regente-workspace
                              ▼
                          agent in a Docker CONTAINER (disposable)
                              • COMMAND/SCRIPT/HTTP jobs run IN HERE
                              • no host volumes, non-root, cap-drop, CPU/RAM limits
```

Decisions behind this demo:

- **Cloudflare Tunnel** — free, no account, no card. The link is ephemeral
  (`*.trycloudflare.com`) and changes every time you start it — and you **never have to rebuild**
  the frontend, because it uses `window.location.origin` (any tunnel URL works).
- **Execution in an isolated Docker container** — jobs really run, but they are trapped in a
  disposable container. Your guests' commands **never touch your machine or your files**.
- **GitOps straight into the real `regente-workspace`** — jobs people create become commits
  directly on `main`. Simple and fluid (no PR in the middle). See "Security" below.

## Requirements

- **Go 1.25+** and **Node/npm** (to build the server and the frontend)
- **Docker Desktop RUNNING** (for the sandbox agent)
- **cloudflared** on the PATH: `winget install --id Cloudflare.cloudflared`
- A **GitHub PAT** with **push** permission on `Dr0nj/regente-workspace` (fine-grained:
  Contents = Read and write on that repo). The script reuses the token saved in
  `%LOCALAPPDATA%\regente-lab\github-token.txt` if it exists; otherwise it asks.

## Start it

From the repository root, in PowerShell:

```powershell
.\deploy\demo\host-demo.ps1
```

If you see **"running scripts is disabled on this system"** (the Windows default policy), use one
of these — **neither needs admin rights**:

```powershell
# option A — just this once, changing nothing on the system:
powershell -ExecutionPolicy Bypass -File .\deploy\demo\host-demo.ps1

# option B — allow local scripts for your user permanently (run once):
Set-ExecutionPolicy -Scope CurrentUser RemoteSigned
.\deploy\demo\host-demo.ps1
```

The script builds the frontend (`@origin`), builds and starts the server on `:9091` serving
everything from one origin, builds and starts the agent in Docker, and opens the Cloudflare
Tunnel. **Copy the `https://<...>.trycloudflare.com` link** it prints and send it to your guests.

Initial login: **admin / admin** (it forces a password change on first use).

### Inviting people

In **Settings → Users**, create one account per person and pick the role:

- **operator** — creates and runs jobs (what you want for people who will really try it out)
- **viewer** — observes only (good for someone who will just give visual feedback)

That way everyone logs in with their own account — do not share the admin one.

## Stop it

Close the `cloudflared` window (Ctrl+C) and run:

```powershell
docker rm -f regente-sandbox
Get-Process regente-server -ErrorAction SilentlyContinue | Stop-Process -Force
```

## Security — read this before inviting anyone

You are exposing an orchestrator that **executes commands**. Mitigations already built in:

- Execution happens **inside the container** (no host mount, non-root, `--cap-drop ALL`,
  `--security-opt no-new-privileges`, PID/CPU/RAM limits). Whatever your guests run stays in
  there; `docker rm -f regente-sandbox` tears it down or recycles it.
- **Per-person accounts** with RBAC (operator/viewer) instead of a shared admin.
- A **random** API token per session (the script generates one on every run).

Things to keep in mind (because this demo writes directly to the real repo):

- Jobs people create **commit straight to `main`** in your `regente-workspace`. If someone makes
  a mess, it is a `git revert` in the repo. If you would rather review changes, swap
  `-git-write-mode direct` for `pr-required` in the script (every change then becomes a PR for
  you to approve — that needs a PAT with PR permission).
- The container has outbound network access (HTTP and `COMMAND` jobs can reach the internet). To
  cut that off, run the agent with `--network none` (but then HTTP/network jobs stop working).
- The `trycloudflare.com` link is **public**: anyone with the URL sees the login screen. The
  protection is the login and RBAC — only hand out accounts to people you trust.

Invite a small number of people you trust, and shut the demo down when you are done.
