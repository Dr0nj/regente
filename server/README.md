# regente-server

Production deployments: follow the [production profile](../docs/production-profile.md) for explicit environment/network scope, disabled legacy tokens and conversion steps.

The Go daemon: the control plane. It holds the scheduler, the REST API, the WebSocket hub and
the GitOps layer.

## Architecture

- HTTP REST API (chi) on `:8080`
- A WebSocket hub for the web UI (live events) and for agents (dispatch); agents can also use
  HTTP long-poll or SSE
- The scheduler core running in a goroutine (daily + tick)
- Definitions: YAML at `workspace/definitions/<folder>/<id>.yaml`, optionally committed through
  Git
- Runtime state: SQLite (pure Go, no CGO) or Postgres — chosen with `-db-driver`

## Build

Requires Go 1.25+.

```bash
cd server
go mod tidy
CGO_ENABLED=0 go build -o regente-server .
```

## Run (dev)

```bash
./regente-server -addr :8080 -workspace ./workspace -db ./regente.db -api-token dev-token
```

The most common flags (all of them also read a `REGENTE_*` environment variable, so a systemd
unit or a k8s manifest needs no arguments):

| Flag | Default | What it does |
|---|---|---|
| `-addr` | `:8080` | HTTP listen address |
| `-workspace` | `./workspace` | Path containing `definitions/` |
| `-db` | `./regente.db` | SQLite file path, or the Postgres DSN |
| `-db-driver` | `sqlite` | `sqlite` \| `postgres` |
| `-api-token` | `dev-token` / `REGENTE_TOKEN` | Administrative API bearer; **not** an agent credential or browser session |
| `-spa-dir` | — | Serves the built UI on the same origin (single-origin) |
| `-docs-dir` | — | Serves a generated docs site under `/docs` |
| `-git-source` | — | Workspace repository URL (GitOps) |
| `-git-commit` | `false` | Commits saves (the workspace must be a repo) |
| `-tick-ms` | `2000` | Scheduler tick |
| `-scheduler` | `internal` | `internal` (goroutine ticker) \| `external` (driven over HTTP) |
| `-role` | `all` | `all` \| `api` \| `scheduler` |
| `-auth-mode` | `local` | `local` (password) \| `hybrid` (password + SSO) \| `oidc` (SSO required); see [authentication](../docs/authentication.md) |
| `-bus` | `hub` | `hub` (local) \| `nats` (distributed multi-node hub) |
| `-backup` | — | One-shot mode: writes an online backup and exits |

Run `./regente-server -h` for the full list.

External agents require a separately provisioned machine credential from
**Settings > Agents** or `POST /api/agents/tokens`. Match the issued ID,
environment and capabilities exactly, set the agent's protected `REGENTE_TOKEN`
environment and verify authenticated presence plus a synthetic COMMAND.
The server's administrative bearer and human login sessions are rejected by agent
transports. See [agent identity](../docs/agent-identity.md) and the
[reproducible local demo](../deploy/demo/README.md).

## API

Authentication depends on the caller, not just the `/api` prefix:

| Caller | Credential and policy |
|---|---|
| Browser UI | HttpOnly, SameSite=Lax session cookie; HTTPS uses Secure `__Host-regente_session`, HTTP uses `regente_session`. Cookie requests except GET/HEAD/OPTIONS require `X-CSRF-Token` from login or `GET /api/auth/me` and an allowed origin. |
| API / CLI / MCP | `Authorization: Bearer <token>` from non-browser `POST /api/auth/login` (`{username,password}`), or the admin-equivalent static `REGENTE_TOKEN`, in `local`/`hybrid` mode. |
| External agent | Separately issued machine token on agent transports, bound to ID, environment and capabilities; not a human/API credential. |

`local` allows local passwords; `hybrid` also allows OIDC; `oidc` requires SSO
and rejects ordinary local login and the static admin bearer. Explicit emergency
access is a separately configured, audited 15-minute recovery path, not a routine
integration bypass. Browser login adds `browser:true`, returns an empty JSON
token and sets the cookie; never put that session in bearer headers, URLs or
localStorage. OIDC provider tokens are not Regente API bearer tokens.

Public entry points include `/health`, `/livez`, `/readyz`, `/metrics`, `/api/env`,
`/api/auth/config`, `/api/auth/login`, OIDC login/callback and `/api-docs`.
Signed quick actions validate their scoped link token. Web events require a
30-second single-use ticket from authenticated `POST /api/auth/event-ticket`;
browser callers need CSRF for that POST. See [authentication](../docs/authentication.md),
[agent identity](../docs/agent-identity.md) and [web events](../docs/web-events.md).

The **contract lives in the binary**: a running server serves the curated OpenAPI spec plus a
self-contained viewer at **`/api-docs`**, and the raw spec at `/api-docs/openapi.yaml` and
`/api-docs/openapi.json` (importable into Postman, Insomnia or a code generator). To read it
without starting anything, see the [API reference](https://dr0nj.github.io/regente/api.html) in
the published docs site.

## Extra commands

| Command | What it is for |
|---|---|
| `go run ./cmd/mcp` | MCP server — operate Regente from an AI agent ([docs](../docs/mcp.md)) |
| `go run ./cmd/docsite` | Generates the static documentation site |
| `go run ./cmd/importctm` | Imports a legacy orchestrator export (DEFTABLE XML) into a workspace |
| `go run ./cmd/regente` | Operator CLI (`ops`, `promote`, `dev`, `test`) |

## Production

Run the server **supervised**, with automatic restart — see [`deploy/`](deploy) (systemd
`Restart=always` / Windows Service / a k8s `livenessProbe`), the
[operations guide](../docs/operations.md) and the [DR runbook](../docs/dr-backup.md).
