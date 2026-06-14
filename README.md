<div align="center">
  <img src="app/public/favicon-512.png" width="96" alt="Regente" />
  <h1>Regente</h1>
  <p><strong>Orquestrador de workflows Git-nativo, inspirado em Control-M.</strong></p>
  <p>
    <a href="#instalação">Instalação</a> ·
    <a href="#arquitetura">Arquitetura</a> ·
    <a href="#desenvolvimento">Dev</a> ·
    <a href="app/README.md">Frontend</a>
  </p>
</div>

---

Regente é um orquestrador de jobs onde **o repositório Git é a fonte da verdade**: cada
caixinha na tela vira um YAML commitado, e cada YAML no GitHub vira uma caixinha. A UX é a
de quem opera Control-M (folders, monitoring do dia, hold/rerun/force, find & update), e a
arquitetura nasce **local-first** (roda nas suas máquinas via agente) com caminho pronto
para serverless AWS.

Este é o **monorepo** do projeto inteiro:

| Pasta | Componente | Stack |
|---|---|---|
| [`server/`](server) | **regente-server** — daemon (API REST + WebSocket hub, scheduler, GitOps) | Go |
| [`agent/`](agent) | **regente-agent** — executor que roda nas suas máquinas (COMMAND/SCRIPT/HTTP) | Go |
| [`app/`](app) | **regente-web** — frontend (Monitoring, Design canvas) | React + TypeScript + Vite |

> A fonte da verdade dos jobs (YAMLs das *definitions*) mora num repositório **separado**
> de workspace GitOps — este repo é só o **código** da plataforma.

## Capacidades

- **Git-nativo (GitOps)** — Publish vira commit/PR; mudanças no GitHub voltam pra UI via webhook + polling.
- **Executores locais via agente** — `COMMAND`, `SCRIPT` (.sh/.bat/.ps1), `HTTP`; targeting por agente ou capability; tokens por agente; instalável como serviço.
- **SSH agentless** — comando remoto direto do server.
- **Daily imutável** — instances congeladas no momento da ordem (mudança publicada no dia só entra na próxima daily ou via Force).
- **Paridade Control-M** — calendars, resources/quotas, conditions, variáveis globais, SLA, forecast.
- **Enterprise readiness** — backend **Postgres** (além de SQLite), **leader election** (HA do scheduler), **secrets manager** plugável, **SSO/OIDC** opt-in. Detalhes em [`server/`](server).

## Instalação

### Opção A — binários prontos (GitHub Releases)
Cada release publica binários do **server** e do **agent** para Linux, Windows e macOS.

```bash
# Linux/macOS — agent
curl -L https://github.com/Dr0nj/regente/releases/latest/download/regente-agent_linux_amd64 -o regente-agent
chmod +x regente-agent
./regente-agent -server ws://SEU_SERVER:8080/ws/agent -token <token> -caps COMMAND,SCRIPT,HTTP
```
```powershell
# Windows — agent como serviço (Tarefa Agendada)
irm https://github.com/Dr0nj/regente/releases/latest/download/install-windows.ps1 | iex
```

### Opção B — compilar do código (precisa de Go 1.25+)
```bash
git clone https://github.com/Dr0nj/regente.git && cd regente

# server
cd server && go build -o regente-server . && ./regente-server -api-token dev-token

# agent (na máquina onde os comandos devem rodar)
cd ../agent && go build -o regente-agent . && \
  ./regente-agent -server ws://localhost:8080/ws/agent -token dev-token -caps COMMAND,SCRIPT,HTTP
```

> `go install github.com/Dr0nj/regente/agent@latest` (instalação direta via Go) fica
> disponível quando os module paths forem alinhados ao monorepo — ver issues/roadmap.

## Arquitetura

```
  ┌──────────────┐    REST + WebSocket    ┌──────────────────┐   git push/pull   ┌──────────┐
  │  app/         │ ─────────────────────▶ │  server/          │ ◀───────────────▶ │  GitHub   │
  │  (React)      │ ◀───────────────────── │  (Go, SQLite/PG)  │   (fonte da       │  (YAML)   │
  └──────────────┘    instance.changed     └──────────────────┘    verdade)        └──────────┘
                                                    ▲
                                                    │ WebSocket (agente disca pra fora — NAT-friendly)
                                                    │ dispatch ▼   ▲ result
                                            ┌──────────────────┐
                                            │  agent/           │  roda COMMAND/SCRIPT/HTTP
                                            │  (seu PC / EC2)   │  no Windows ou Linux
                                            └──────────────────┘
```

- **Local-first:** o agente faz conexão **outbound** pro server (atravessa NAT/firewall), recebe dispatch e devolve o resultado/stream.
- **State store plugável:** SQLite (default, pure-Go, zero infra) ou Postgres (HA/escala) — mesma base de código, via `-db-driver`.
- **HA:** com Postgres, vários servers usam *leader election* (advisory lock) — só o líder materializa a daily; todos servem API.

## Desenvolvimento

```bash
# 1. server
cd server && go run . -api-token dev-token

# 2. agent (terminal separado)
cd agent && go run . -server ws://localhost:8080/ws/agent -token dev-token -id meu-pc -caps COMMAND,SCRIPT,HTTP

# 3. frontend (terminal separado)
cd app && npm install && cp .env.example .env && npm run dev    # http://localhost:5173
```
Login dev: `admin` / `admin`. Testes: `go test ./...` em `server/` e `agent/`.

---

<sub>Projeto pessoal de portfólio. UX inspirada na operação do Control-M; nenhuma relação com a BMC.</sub>
