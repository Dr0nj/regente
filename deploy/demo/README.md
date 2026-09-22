# Reproducible local demo

The launcher builds a same-origin SPA and server, provisions a separate machine
credential, and waits for authenticated agent presence. It **does not open a
public tunnel**. Each run uses a new temporary DB/workspace, unique agent/container
IDs and an eight-hour credential. It never kills servers by name or reuses a
previous demo database or saved PAT.

## Requirements

- Go 1.25+, Git and Node.js: `^20.19.0 || ^22.13.0 || >=24` (Node 24 in CI).
- Windows PowerShell 5.1 or PowerShell 7.
- A running **Linux Docker engine** (Docker Desktop on Windows), for guest jobs.
  The native smoke below does not require Docker and is **not** a guest demo mode.

From the repository root:

```powershell
.\deploy\demo\host-demo.ps1
# Optional: YOUR workspace, with GITHUB_TOKEN supplied through a protected environment.
# This permits direct commits to that real repository; do not use a production workspace.
.\deploy\demo\host-demo.ps1 -GitRepo owner/demo-workspace -GitBranch main
```

If local execution policy blocks the script, use a per-process invocation:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\deploy\demo\host-demo.ps1
```

Open the printed localhost URL. Change the initial `admin / admin` password
immediately; create individual operator/viewer accounts for trusted guests.
Set jobs' **environment to `demo`** and target the printed agent ID. The issued
scope is exactly `demo` + `COMMAND,SCRIPT,HTTP`; administrative or human tokens
cannot authenticate as this agent. Credentials are passed through process/container
environment, not printed or included in CLI arguments. Docker administrators and
jobs in that container can inspect its environment: it is not a secrets boundary.

The default bind address is `127.0.0.1`. On Windows, Docker Desktop connects through
`host.docker.internal`; if that installation cannot reach a loopback-bound host
service, explicitly choose `-BindAddress 0.0.0.0` **only with a host firewall
restricting access**. That exposes the listener to host interfaces; the launcher
does not configure a firewall. The Linux laboratory uses host networking and
loopback. Neither topology provides job-only network isolation.

No Git source means **offline** local definitions. `-GitRepo` supplies an explicit
HTTPS Git source as well as owner/repository metadata; `-github-repo` alone would
not enable synchronization. The launcher does not read a saved PAT file. See
[operations](../../docs/operations.md) for origin/branch/credentials.

Press Enter in the launcher to stop it. Its `finally` block revokes the credential,
stops only captured process IDs and removes only its uniquely named container and
image. Data/logs remain in the printed private temporary directory; do not publish
that directory. A forced process termination or machine crash can bypass cleanup;
use the exact run-specific resource names for manual cleanup, never a wildcard or
`Get-Process regente-server | Stop-Process`.

## Automated smoke — no tunnel, no real workspace

```powershell
# Real Docker agent: fresh offline workspace, COMMAND dispatch/result, then cleanup.
.\deploy\demo\host-demo.ps1 -Smoke -Port 0
# Explicit Git origin: a synthetic LOCAL repository, never GitHub.
.\deploy\demo\host-demo.ps1 -Smoke -GitFixture -Port 0
# Windows without Docker: only the fixed synthetic echo job executes on this host.
.\deploy\demo\host-demo.ps1 -Smoke -NativeSmoke -GitFixture -Port 0
```

`-NativeSmoke` is rejected without `-Smoke`. Smoke rejects `-GitRepo`, clears
inherited Regente/Vite/telemetry/token configuration and restores the caller's
environment afterward. It runs `npm ci`, builds with `@origin`, provisions the
machine principal, verifies authenticated presence and a pinned COMMAND's actual
output, tests rejected ID/environment/capability claims and administrative bearer,
then revokes the credential and verifies removal/rejection. Every wait has a
timeout and failure is nonzero. Only the sanitized `evidence.json` is suitable
for sharing; private logs and DBs are not CI artifacts.

CI runs the PowerShell launcher on Windows (native synthetic job) and Linux
(real Docker agent), with offline and explicit local Git fixtures. The browser
test separately loads the built SPA from Go and verifies same-origin login/API
requests instead of silent localStorage mode.

## Security limits

The container is non-root, has no host mounts, drops capabilities and limits
CPU/RAM/PIDs. These reduce exposure; they **do not guarantee safety against hostile
code**. Jobs and the agent share credentials and a network namespace.

`--network none` disconnects WS/HTTP/SSE, dispatch and results, not only network
jobs. This recipe does **not** implement job-only egress isolation. Only run jobs
from people you trust. Do not mount the Docker socket, host directories or secrets.

Public hosting is a separate deliberate deployment: first configure passwords,
accounts, TLS, ingress and credential lifecycle using the
[VPS guide](../vps/README.md). No tunnel or public resource is created automatically.
