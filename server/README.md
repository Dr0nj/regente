# regente-server

Orquestrador daemon em Go. Substitui o scheduler que antes rodava no browser.

## Arquitetura

- HTTP REST API (chi) em `:8080`
- WebSocket hub para web (eventos live) e agents (dispatch)
- Scheduler core rodando em goroutine (daily + tick)
- Storage: YAML em `workspace/definitions/<team>/<id>.yaml`, opcionalmente commitado via git
- State runtime: SQLite (pure-Go, sem CGO)

## Build

Pré-requisito: Go 1.23+ (`winget install GoLang.Go`).

```powershell
cd projects/Regente/server
go mod tidy
go build -o ../../../bin/regente-server.exe .
```

## Run (dev)

```powershell
./bin/regente-server.exe `
  -addr :8080 `
  -workspace ./projects/Regente/workspace `
  -db ./regente.db `
  -api-token dev-token
```

Flags:

| Flag            | Default               | Descrição                               |
|-----------------|-----------------------|-----------------------------------------|
| `-addr`         | `:8080`               | HTTP listen                             |
| `-workspace`    | `./workspace`         | Path com `definitions/`                 |
| `-db`           | `./regente.db`        | SQLite path                             |
| `-tick-ms`      | `2000`                | Scheduler tick                          |
| `-git-commit`   | `false`               | Commita saves (workspace deve ser repo) |
| `-api-token`    | `dev-token` / `REGENTE_TOKEN` | Bearer token web + agent        |

## API resumo

Todas as rotas `/api/*` exigem `Authorization: Bearer <token>`.

| Método | Path                                  | Descrição                      |
|--------|---------------------------------------|--------------------------------|
| GET    | `/health`                             | healthcheck                    |
| GET    | `/api/definitions`                    | lista YAMLs de `definitions/`  |
| POST   | `/api/definitions`                    | cria/atualiza YAML             |
| DELETE | `/api/definitions/{team}/{id}`        | remove YAML                    |
| GET    | `/api/folders`                        | lista subdirs de `definitions/`|
| POST   | `/api/folders`                        | cria subdir                    |
| GET    | `/api/instances?date=YYYY-MM-DD`      | instances do dia               |
| POST   | `/api/instances/{id}/hold`            | Hold                           |
| POST   | `/api/instances/{id}/release`         | Release                        |
| POST   | `/api/instances/{id}/cancel`          | Cancel                         |
| POST   | `/api/instances/{id}/rerun`           | Rerun                          |
| POST   | `/api/daily/run`                      | força a daily do dia           |
| POST   | `/api/definitions/{id}/force`         | Order Force (Control-M)        |
| GET    | `/api/agents`                         | agents online                  |
| GET    | `/ws/web?token=...`                   | WS para web (events)           |
| GET    | `/ws/agent?token=...&id=...&caps=...` | WS para agent (dispatch)       |

## Próximos passos

Ver roadmap F10→F21 no `../README.md`.
