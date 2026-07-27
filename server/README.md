# regente-server

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
| `-api-token` | `dev-token` / `REGENTE_TOKEN` | Bearer token for the web UI and agents |
| `-spa-dir` | — | Serves the built UI on the same origin (single-origin) |
| `-docs-dir` | — | Serves a generated docs site under `/docs` |
| `-git-source` | — | Workspace repository URL (GitOps) |
| `-git-commit` | `false` | Commits saves (the workspace must be a repo) |
| `-tick-ms` | `2000` | Scheduler tick |
| `-scheduler` | `internal` | `internal` (goroutine ticker) \| `external` (driven over HTTP) |
| `-role` | `all` | `all` \| `api` \| `scheduler` |
| `-auth-mode` | `local` | `local` \| `oidc` (opt-in SSO) |
| `-bus` | `hub` | `hub` (local) \| `nats` (distributed multi-node hub) |
| `-backup` | — | One-shot mode: writes an online backup and exits |

Run `./regente-server -h` for the full list.

## API

Every `/api/*` route requires `Authorization: Bearer <token>`.

The **contract lives in the binary**: a running server serves the curated OpenAPI spec plus a
self-contained viewer at **`/api-docs`**, and the raw spec at `/api-docs/openapi.yaml` and
`/api-docs/openapi.json` (importable into Postman, Insomnia or a code generator). To read it
without starting anything, see the [API reference](../docs/site/api.html) in the generated docs
site.

## Extra commands

| Command | What it is for |
|---|---|
| `go run ./cmd/mcp` | MCP server — operate Regente from an AI agent ([docs](../docs/mcp.md)) |
| `go run ./cmd/docsite` | Generates the static documentation site |
| `go run ./cmd/importctm` | Imports a Control-M export (DEFTABLE XML) into a workspace |
| `go run ./cmd/regente` | Operator CLI (`ops`, `promote`, `dev`, `test`) |

## Production

Run the server **supervised**, with automatic restart — see [`deploy/`](deploy) (systemd
`Restart=always` / Windows Service / a k8s `livenessProbe`), the
[operations guide](../docs/operations.md) and the [DR runbook](../docs/dr-backup.md).
