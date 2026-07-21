<div align="center">
  <img src="app/public/logo-r.png" width="92" alt="Regente" />
  <h1>Regente</h1>
  <p><strong>Orquestrador de workflows Git-nativo, inspirado em Control-M.</strong></p>
  <p>
    <a href="#-conceito">Conceito</a> ·
    <a href="#-capacidades">Capacidades</a> ·
    <a href="#-arquitetura">Arquitetura</a> ·
    <a href="#-instalação">Instalação</a> ·
    <a href="#-server">Server</a> ·
    <a href="#-agent">Agent</a> ·
    <a href="#-frontend">Frontend</a> ·
    <a href="#-desenvolvimento">Dev</a> ·
    <a href="#-roadmap">Roadmap</a>
  </p>
</div>

---

Regente é um orquestrador de jobs onde **o repositório Git é a fonte da verdade**: cada
caixinha na tela vira um YAML commitado, e cada YAML no GitHub vira uma caixinha. A UX é a
de quem opera Control-M (folders, monitoring do dia, hold/rerun/force, find & update), e a
arquitetura nasce **local-first** (roda nas suas máquinas via agente) com caminho pronto
para serverless AWS.

Este é o **monorepo** do projeto inteiro. Cada componente tem seu próprio README
([`server/`](server/README.md) · [`agent/`](agent/README.md) · [`app/`](app/README.md)),
mas **este README reúne tudo** — é o compilado completo da plataforma.

| Pasta | Componente | Stack |
|---|---|---|
| [`server/`](server) | **regente-server** — daemon (API REST + WebSocket hub, scheduler, GitOps) | Go |
| [`agent/`](agent) | **regente-agent** — executor que roda nas suas máquinas (COMMAND/SCRIPT/HTTP) | Go |
| [`app/`](app) | **regente-web** — frontend (Monitoring, Design canvas) | React + TypeScript + Vite |

> A fonte da verdade dos jobs (YAMLs das *definitions*) mora num repositório **separado**
> de workspace GitOps — este repo é só o **código** da plataforma.

---

## 🧭 Conceito

Dois mundos separados, estilo Control-M:

- **Monitoring** — o que está rodando hoje. Resultado da *Daily*. Só consumo:
  hold (qualquer status, com release restaurando o original) / release / cancel / set-ok / rerun / delete (em hold) / force order, audit por instance, SLA.
  É um **snapshot imutável**: a folder de cada instância é congelada quando a daily
  foi schedulada — apagar ou mover o job no Design não reescreve a daily corrente.
- **Design** — onde as definitions são editadas. Pelo botão **FOLDERS** você cria,
  abre/fecha (multi-select) e gerencia *folders* — abrir uma folder monta um clone
  Git efêmero por sessão; você edita no canvas drag-and-drop e dá **Publish**
  (único caminho de escrita pro GitHub). **Drafts não se perdem**: F5/troca de
  tela retoma a sessão onde parou; sessão esquecida com edições não publicadas
  vira banner *Retomar/Descartar* (o server nunca apaga trabalho não publicado —
  só sessões limpas expiram).

**A Daily** roda 1×/dia à meia-noite (BRT): lê o Git, decide o que roda hoje
(schedule + calendars + dependências + conditions) e materializa *instances*
**imutáveis** — uma mudança publicada no Design durante o dia só entra na próxima
daily ou via Force Order manual.

---

## ⚡ Capacidades

- 🟢 **Git-nativo (GitOps)** — Publish vira commit/PR; mudanças no GitHub voltam
  pra UI via webhook + polling. Deep-links job→YAML, instance→commit.
- 🟢 **Executores locais via agente** — `COMMAND` (shell), `SCRIPT` (.sh/.bat/.ps1)
  e `HTTP`, rodando na máquina onde o agente está (Windows ou Linux). Cada job
  pode mirar um agente específico ou ser roteado por capability. Tokens **por
  agente** + agente instalável como serviço (systemd / Tarefa Agendada).
  **Pin estrito:** job pinado num agente roda SÓ nele — se o agente cair, o job
  espera em WAIT AGENT até ele voltar (nunca migra sozinho).
- 🟢 **SERVER-AGENT embutido** — todo server nasce com um agente padrão de
  `HTTP`/`REST`: chamadas de API rodam direto do server, sem instalar agente
  externo. Aparece na tela de Agentes e é pinável no Design como qualquer outro
  (desligável via `-server-agent=false`).
- 🟢 **SSH agentless** — `SSH` roda comando remoto direto do server (sem agente
  no alvo), com stream de saída.
- 🟢 **Daily imutável** — instances congeladas no momento da ordem (mudança
  publicada no dia só entra na próxima daily ou via Force).
- 🟢 **Ciclo de vida da daily (carry-over Control-M)** — na virada, a ordem que
  ainda está aberta **não some**: RUNNING e HELD atravessam sempre; NOTOK
  não-tratado persiste +1 diária (ou N via `schedule.keepActive`); OK/CANCELLED
  encerram. A ordem **avança o order_date** mantendo id/status/histórico (não
  duplica) e exibe a origem (badge ↩). Idempotente.
- 🟢 **Explain — "por que esse job não rodou?"** (diferencial, sem IA): por instância,
  expõe o gating que o scheduler já computa — janela, dependências (qual upstream/condição),
  conditions faltando, recurso indisponível (quer/uso/capacidade). Construído como **fonte
  única**: o mesmo avaliador decide o dispatch e explica, então condição nova é absorvida sem
  manutenção dupla. `GET /api/instances/{id}/explain` + painel no drawer.
- 🟢 **Diff de Daily** (diferencial Git-native): compara duas diárias (hoje vs a anterior,
  ou `?from&to&folder`) → jobs **adicionados / removidos / alterados**, com diff **por-campo**
  (schedule, deps, recursos, def). Lê os snapshots congelados (`commit_sha` + `definition_snapshot`)
  → exato e barato, sem reprocessar Git. `GET /api/daily/diff` + aba "Diff" no Control Panel.
- 🟢 **Blast Radius** (diferencial): "se eu **cancelar/segurar** este job agora, qual o impacto?"
  → jobs downstream que deixam de rodar em cascata, SLAs em risco, folders afetadas e profundidade.
  Análise de uma AÇÃO (não do grafo estático): BFS no grafo de deps, só pelo raio. `GET
  /api/instances/{id}/blast-radius` + painel "⚠ Impacto" no drawer.
- 🟢 **Dry Run** (diferencial): **simula a daily de qualquer data sem materializar** — quem **roda**,
  quem **espera** (depois de quem) e quem **nunca dispara** (e por quê: fora do calendário, dependência
  que não roda, condition órfã…), com cascata transitiva. Reusa a mesma decisão de agendamento do RunDaily.
  `GET /api/daily/dryrun?date=` + aba "Dry Run" no Control Panel (com seletor de data).
- 🟢 **What-If** — *"e se o job X atrasar 40min / demorar o dobro / falhar / não rodar?"*: projeta a
  diária **baseline × cenário** com durações reais (p50 do histórico) e a semântica de deps do engine
  (falha simulada bloqueia on-success e **destrava** o recovery on-failure) → quem atrasa, quem
  bloqueia, que SLA passa a estourar. Read-only, nada é materializado. `POST /api/whatif` + aba
  "What-If" no Control Panel. **Statistics** por job (taxa de sucesso, min/avg/p50/p90/max) na aba Stats
  do drawer (`GET /api/analytics/jobstats`).
- 🟢 **Schema dedicado por jobType** — cada tipo tem contrato declarado de params (obrigatórios,
  tipos de valor, enums, aliases): typo de campo e valor errado acusados **na hora** (lint/save);
  obrigatórios cobrados no **publish** (422 listando job a job). Catálogo em `GET /api/jobtypes`.
- 🟢 **Multi-ambiente / multi-site** — `environment` no job **roteia a execução**: agente com
  `-env prod` só recebe jobs daquele ambiente (agente sem label = generalista; vale cross-nó no
  cluster). Sem agente casando, o job espera em WAIT AGENT com o motivo no Explain. Combina com o
  `regente promote` (Dev→Staging→Prod, Git-native).
- 🟢 **Agent-native (MCP)** — servidor [MCP](https://modelcontextprotocol.io) (`server/cmd/mcp`)
  que expõe **22 tools**: você **opera o Regente conversando** com o Claude (*"o que falhou em
  pagamentos hoje e por quê?"*, *"prevê a próxima semana"*). **11 de leitura** (summary · forecast ·
  explain · blast radius · vizinhança · causa raiz · diff · dry run · event log · NL-query) sempre
  disponíveis; **11 de escrita** (hold/release/cancel/confirm/rerun/set-ok · **force order** ·
  **pause/resume de folder** · **bulk** · **ingest de evento**) atrás de `-allow-writes` + aprovação
  do cliente (dupla trava, cada uma `destructiveHint`). Pure-Go stdlib, fachada sobre a REST. Ver
  [`docs/mcp.md`](docs/mcp.md).
- 🟢 **Tela de Agentes** (Settings → Agentes) — frota **consolidada** (online + offline com last-seen) +
  contador "N de M online"; clique num agente abre um **modal de detalhe** (SO/arch, host, versão, **uptime**,
  conectado há, 1ª vez visto, último sinal, capabilities). O agente reporta a metadata no handshake; gestão
  de tokens junto. **Ping ativo** (round-trip ping/pong, latência) por agente e "ping todos". **Cross-nó (R5):**
  com `-bus=nats` a frota reflete o **cluster inteiro** — um agente conectado em OUTRO nó aparece online (com o
  nó dono), não fantasma offline (só os do próprio nó são pingáveis). `GET /api/agents` · `POST /api/agents/{id}/ping`.
- 🟢 **Retry de execution** — re-tentativa automática em falha (respeita `retries`).
- 🟢 **Observabilidade** — `/metrics` em formato Prometheus.
- 🟢 **Alerting (Fase 8)** — regras configuráveis avaliadas ao fim de cada
  execução (falha / lentidão / retries / taxa de sucesso / falhas consecutivas);
  tela de alertas (sino + badge, ack individual/todos) e toast em tempo real.
  **Routing multi-canal por regra**: Slack · webhook genérico · e-mail (SMTP) ·
  PagerDuty Events API. **Cooldown por (regra×job)** — uma rajada de jobs distintos
  nunca perde alertas (só re-disparos do mesmo job são agrupados). **Ciclo de vida
  do alerta** — rerun e Set OK do job marcam o alerta como *tratado* (rerun / set ok)
  automaticamente; re-falha gera um alerta novo. Server mode (Postgres) e local.
- 🟢 **Schedule estilo Control-M** — frequência (diário / dias da semana / dias do mês),
  janelas e execução cíclica. Os **calendários** vivem na própria aba Schedule e trabalham
  junto com as regras como **include/exclude** (ex.: negar "dias úteis" + todos os dias =
  roda todo dia, exceto dia útil), com **tradução** do que cada calendário faz e um
  **preview de calendário REAL** no rodapé (estilo Control-M "View Scheduling": 12
  mini-meses + seletor de ano; os dias **destacados** são exatamente os que o job vai
  rodar, calculados pelo backend com a **mesma regra da daily** — `POST /api/schedule/preview`).
  ("Dia útil"/regra avançada saíram da frequência: dia útil depende do calendário de feriados de cada lugar.)
- 🟢 **Dependências = CONDIÇÕES num pool único** (Control-M global conditions):
  ligar A→B no canvas cria a condição `A-TO-B` — saída＋ no pai, entrada +
  saída− (consumo) no filho; digitar à mão vai pro MESMO lugar. O job roda
  quando as entradas existem no pool; o OK (ou Set OK) adiciona as saídas＋ e
  **deleta** as saídas− (por isso Set OK + rerun volta a aguardar). O painel
  **Condições** do Monitoring lista o pool inteiro com data — deletar ali trava
  quem depende; adicionar libera na hora. **Data de referência Control-M** por
  linha — `Odate` (diária de origem, default), `Prev` (anterior) e `Stat`
  (estática, sem data). O carry-over preserva o **ODAT**: um job carregado do
  dia 14 segue "do dia 14" para condições, linhas do grafo e Active Jobs.
  **Lógica AND/OR na entrada**: por padrão a entrada é um AND (todas exigidas), mas
  o toggle **AND/OR** no drawer agrupa as condições — cada grupo com seu operador
  (AND/OR) + um operador de topo entre grupos, ex. `(C1 AND C2) OR C3` (roda pelo
  primeiro ramo que fechar). Com uma condição, o atalho **"OR rodar no horário"**
  roda quando a condição chega OU quando o horário "a partir de" é atingido — o
  que vier primeiro (sem janela definida, o atalho leva à aba Horário). As linhas
  OR do grafo ficam pontilhadas com rótulo **OR**.
- 🟢 **Aprofundamento Control-M** — **execução cíclica** (o job re-arma sozinho a cada
  N min dentro da janela, com teto de voltas), **Confirm** (job só roda após liberação
  manual do operador — gate `WAIT_CONFIRM`, nem o Force bypassa), job **DATABASE** (SQL
  em Postgres/MySQL/SQLite pelo agente, sem client instalado), **variáveis SET em runtime**
  (um job imprime `%%SET NOME=VALOR` e outro lê) e **cálculo de datas** `%%ODATE±N`/`±NB`
  (offset em dias corridos ou úteis, ciente do calendário do job). Janela que fecha vira
  gate `WINDOW_CLOSED`. Toda a lógica de gating passa pela **mesma fonte única** do Explain.
- 🟢 **Engines de paridade** — calendars (dias úteis N-ésimos, 1º dia útil,
  include/exclude, feriados, meses), resources/quotas (lock exclusivo, pool com
  fila), conditions, variáveis globais (interpolação), SLA e forecast/analytics
  (dry-run **≥1 semana à frente** pelo mesmo gating da daily).
- 🟢 **Token do GitHub pela UI** — configurável em runtime (Settings), persistido
  server-side, sem precisar subir o server com `GITHUB_TOKEN`.
- 🟢 **Enterprise readiness** — backend **Postgres** (além de SQLite), **leader
  election** (HA do scheduler), **secrets manager** plugável, **SSO/OIDC** opt-in.
- 🟢 **Identidade visual** — logotipo **R** próprio (branco, vetorial, fundo
  transparente), topbar **premium flutuante** (cantos arredondados, segmented
  `‹ DESIGN · MONITORING ›` com chevrons e destaque **neon** que segue o tema) e cluster
  de ações à direita: **alertas** (sino), **configurações/conta** (engrenagem → menu),
  **tela cheia** (monitor) e **avatar**. Tela de **login** (paleta dark blue neon),
  **sidebars flutuantes** e **janela de info do job dockada** (mesmo estilo do ACTIVE JOBS),
  com realce **neon** no selecionado. **Diálogos padronizados** (✕ + Cancelar/Salvar via
  classes compartilhadas; "Control-M Panel" → **Control Panel**). O **pan fica travado** nos
  dois modos, entrando **ancorado** um pouco abaixo do topo; no **Design** é **limitado** à
  caixa dos jobs da folder (só puxa pros lados quando passam da tela, sem "se perder"). Há um
  **minimap de navegação** opcional (protótipo, Settings → Geral; ponto por job, clique navega).
- 🟢 **Temas** — aparência configurável em Settings → aba **Temas**. **13 temas**: **Escuro**
  (padrão), **Verde Amarelo**, **Amarelo Ouro**, **Verde Mata**, **Azul Neon**, **Azul Escuro**,
  **Rosa**, **Violeta**, **Vermelho**, **Laranja**, **Cinza** (escala), **Bege Escuro** e **Marrom**.
  Cada card mostra um **swatch** com as cores do tema.
  Aplica na hora e persiste no navegador (`localStorage`). Implementação: cada tema
  sobrescreve os design tokens `--v2-*` via `:root[data-theme="<id>"]` em
  `app/src/v2/tokens.css` (catálogo em `app/src/lib/theme.ts`). Toda a UI consome esses
  tokens — inclusive o painel **Control-M Parity**. Diálogos de config/senha/tema ganham
  **borda neon na cor do tema** (`.v2-neon-card`).

---

## 🏗 Arquitetura

```
  ┌──────────────┐    REST + WebSocket    ┌──────────────────┐   git push/pull     ┌──────────┐
  │  app/        | ────────────────────▶ |  server/          │ ◀───────────────▶ │  GitHub   │
  │  (React)     | ◀─────────────────────|  (Go, SQLite/PG)  │   (fonte da        │  (YAML)   │
  └──────────────┘    instance.changed    └──────────────────┘    verdade)         └──────────┘
                                                    ▲
                                                    │ WebSocket (agente disca pra fora — NAT-friendly)
                                                    │ dispatch ▼   ▲ result
                                            ┌──────────────────┐
                                            │  agent/          |  roda COMMAND/SCRIPT/HTTP
                                            │  (seu PC / EC2)  |  no Windows ou Linux
                                            └──────────────────┘
```

- **Local-first:** o agente faz conexão **outbound** pro server (atravessa NAT/firewall),
  recebe dispatch e devolve o resultado/stream.
- **State store plugável:** SQLite (default, pure-Go, zero infra) ou Postgres (HA/escala) —
  mesma base de código, via `-db-driver`.
- **HA:** com Postgres, vários servers usam *leader election* (advisory lock) — só o líder
  materializa a daily; todos servem API.

---

## 📦 Instalação

> **Deploy padrão = systemd** — supervisão `Restart=always`, estado persistente em
> `/var/lib/regente` (sobrevive a reboot/upgrade). Artefatos em [`server/deploy/`](server/deploy)
> e [`agent/deploy/`](agent/deploy). Escolha **uma das três formas** conforme o que roda no host.
> Cada uma vem em dois sabores: **release** (`install.sh` baixa o bundle pronto — sem Go/Node) ou
> **código-fonte** (Go 1.25+, e Node para a UI). Windows: `install-windows.ps1` em cada `deploy/`.

### Forma 1 — só o server (API headless)
Control plane **sem UI embutida** (acesso via API/CLI, ou UI servida em outro host).
```bash
# release (sem toolchain):
curl -fsSL https://github.com/Dr0nj/regente/releases/latest/download/install.sh -o regente-install.sh
sudo WITH_UI=0 bash regente-install.sh
# ou do código-fonte:
cd server && CGO_ENABLED=0 go build -o regente-server . && sudo WITH_UI=0 ./deploy/install-linux.sh
```

### Forma 2 — server + UI (single-origin) — recomendado p/ VPS
UI + API + WebSocket na **mesma porta/origem** (sem CORS; o front resolve `@origin` em runtime).
```bash
# release (binário + UI + systemd, tudo pronto):
curl -fsSL https://github.com/Dr0nj/regente/releases/latest/download/install.sh -o regente-install.sh
sudo bash regente-install.sh
# ou do código-fonte (builda a UI e liga sozinho):
cd server && CGO_ENABLED=0 go build -o regente-server .
(cd ../app && VITE_REGENTE_SERVER_URL=@origin npm ci && npm run build)
sudo ./deploy/install-linux.sh
```
Depois (as duas formas): `sudo regente-configure` (assistente: token forte, GitHub PAT/repo,
domínio) — ou edite `/etc/regente/server.env` à mão — → `sudo systemctl restart regente-server`
→ `http://<host>:8080` (login `admin`/`admin`).

### Forma 3 — server + agente (mesma caixa, lab single-box)
O server **e** um agente local — a própria caixa também executa jobs. (Em produção os agentes
ficam nas **outras** máquinas; co-locar é conveniência de lab.)
```bash
# 1) suba o server (Forma 1 ou 2). 2) instale um agente local:
cd agent && CGO_ENABLED=0 go build -o regente-agent .
sudo SERVER=ws://localhost:8080/ws/agent TOKEN=<token> ID=$(hostname) \
     CAPS=COMMAND,SCRIPT,HTTP ./deploy/install-linux.sh
# TOKEN: gere em Settings → Agentes (ou use o REGENTE_TOKEN em dev).
```

> **Domínio + HTTPS (link público, estilo empresa):** ponha o server atrás de um reverse
> proxy (nginx) com TLS e um domínio real — receita systemd completa em
> [`deploy/vps/`](deploy/vps) (§ *Hospedagem enterprise*).

### Agentes nas outras máquinas (Linux · macOS · Windows) — serviço 24/7

Instaladores **amistosos**: baixam o binário da release e registram o agente como serviço
(**systemd** · **launchd** · **Tarefa Agendada**) — inicia no boot, reinicia se cair, roda
sem ninguém logado. **Sem Docker, Go ou runtime.** Perguntam servidor + token se você não passar.

```bash
# Linux ou macOS — baixe e rode (ele pergunta o servidor e o token)
curl -fsSL https://github.com/Dr0nj/regente/releases/latest/download/install-agent.sh -o install-agent.sh
sudo bash install-agent.sh
# frota / silencioso:  sudo SERVER=wss://SEU-DOMINIO/ws/agent TOKEN=rgta_xxx bash install-agent.sh
```
```powershell
# Windows (PowerShell como Administrador) — pergunta o servidor e o token
irm https://github.com/Dr0nj/regente/releases/latest/download/install-agent-windows.ps1 | iex
# frota / silencioso: baixe o .ps1 e rode com  -Server wss://... -Token rgta_xxx
```

Gere o **token do agente** em Settings → Agentes. O agente conecta **outbound** (nenhuma porta
aberta na máquina dele — atravessa NAT/firewall); só precisa alcançar o servidor. A frota
aparece em Settings → Agentes.

> `go install github.com/Dr0nj/regente/agent@latest` (instalação direta via Go) fica
> disponível quando os module paths forem alinhados ao monorepo — ver issues/roadmap.

---

## 🖥 Server

> Pasta [`server/`](server) · daemon Go. Substitui o scheduler que antes rodava no browser.

**Componentes:**
- HTTP REST API (chi) em `:8080`
- WebSocket hub para web (eventos live) e agents (dispatch)
- Scheduler core rodando em goroutine (daily + tick)
- Storage: YAML em `workspace/definitions/<team>/<id>.yaml`, opcionalmente commitado via git
- State runtime: SQLite (pure-Go, sem CGO) ou Postgres (`-db-driver`)

### Build & run (dev)

```bash
cd server
go mod tidy
go build -o regente-server .
./regente-server -addr :8080 -workspace ./workspace -db ./regente.db -api-token dev-token
```

### Flags

| Flag            | Default                       | Descrição                               |
|-----------------|-------------------------------|-----------------------------------------|
| `-addr`         | `:8080`                       | HTTP listen                             |
| `-workspace`    | `./workspace`                 | Path com `definitions/`                 |
| `-db`           | `./regente.db`                | Caminho do SQLite (ou DSN do Postgres)  |
| `-db-driver`    | `sqlite`                      | `sqlite` \| `postgres`                  |
| `-tick-ms`      | `2000`                        | Scheduler tick                          |
| `-git-commit`   | `false`                       | Commita saves (workspace deve ser repo) |
| `-api-token`    | `dev-token` / `REGENTE_TOKEN` | Bearer token web + agent                |
| `-auth-mode`    | `local`                       | `local` \| `oidc` (SSO opt-in)          |
| `-bus`          | `hub`                         | `hub` (local) \| `nats` (hub distribuído multi-nó, R5) |

> **Produção:** rode o server **supervisionado** com restart automático — ver
> [`server/deploy/`](server/deploy) (systemd `Restart=always` / Windows Service / `livenessProbe`).
> Todas as flags acima leem variáveis de ambiente (`REGENTE_*`), então a unit/manifesto
> dispensa argumentos. **Multi-nó (R5):** `-bus=nats` faz fan-out de eventos web e roteia o
> dispatch ao nó dono do agent — o failover do líder não estranda agentes.

### API resumo

Todas as rotas `/api/*` exigem `Authorization: Bearer <token>`.

> **Contrato OpenAPI:** o server serve em **`/api-docs`** o contrato curado da superfície de
> integração (spec escrita à mão + viewer self-contained com try-it, embutidos no binário — zero
> CDN, zero flag). `/api-docs/openapi.yaml` e `/api-docs/openapi.json` servem a spec crua
> (importável no Postman/Insomnia/codegen). A tabela abaixo é só um resumo de orientação.

| Método | Path                                  | Descrição                      |
|--------|---------------------------------------|--------------------------------|
| GET    | `/health`                             | healthcheck                    |
| GET    | `/metrics`                            | métricas Prometheus            |
| GET    | `/api/definitions`                    | lista YAMLs de `definitions/`  |
| POST   | `/api/definitions`                    | cria/atualiza YAML             |
| DELETE | `/api/definitions/{team}/{id}`        | remove YAML                    |
| GET    | `/api/folders`                        | lista subdirs de `definitions/` (+ `layout` da folder) |
| POST   | `/api/folders`                        | cria subdir                    |
| PUT    | `/api/folders/{name}/layout`          | grade da folder no canvas (`.regente-folder.yaml`; `{}` = herda o global) |
| GET    | `/api/instances?date=YYYY-MM-DD`      | instances do dia               |
| POST   | `/api/instances/{id}/hold`            | Hold (qualquer status exceto RUNNING; release restaura o original) |
| POST   | `/api/instances/{id}/release`         | Release                        |
| POST   | `/api/instances/{id}/cancel`          | Cancel                         |
| POST   | `/api/instances/{id}/rerun`           | Rerun                          |
| POST   | `/api/instances/{id}/set-ok`          | Set OK (destrava sucessores)   |
| POST   | `/api/instances/{id}/confirm`         | Confirm (gate WAIT_CONFIRM)    |
| DELETE | `/api/instances/{id}`                 | Delete (Control-M "Delete job" — só com o job em HOLD) |
| POST   | `/api/bulk/instances`                 | ação em lote (hold/release/cancel/rerun/set-ok/confirm/delete), por item |
| GET    | `/api/instances/{id}/explain`         | por que (não) rodou: gating estruturado |
| GET    | `/api/instances/{id}/blast-radius`    | impacto de cancelar/segurar (downstream/SLA) |
| GET    | `/api/daily/diff?from&to&folder`      | diff entre duas diárias (+/−/alterados) |
| GET    | `/api/daily/dryrun?date`              | simula daily futura (roda/espera/nunca) |
| GET    | `/api/forecast?date`                  | forecast de 1 dia (elegíveis · ondas · pico de recursos) |
| GET    | `/api/forecast/range?from&days`       | forecast de N dias à frente (≥1 semana) |
| POST   | `/api/daily/run`                      | força a daily do dia           |
| POST   | `/api/definitions/{id}/force`         | Order Force (Control-M)        |
| POST   | `/api/folders/{name}/pause` · `/resume` | pausa/retoma workflow (dia inteiro↔HELD, carry-over incluso; resume restaura cada status) |
| POST   | `/api/events/ingest`                  | evento externo idempotente (seta conditions / força job) |
| POST   | `/api/scheduler/tick`                 | dispara um ciclo (cron externo, `-scheduler=external`) |
| POST   | `/api/scheduler/daily`                | gatilho de daily dedicado (cron diário, separado do tick) |
| GET    | `/api/alerts`                         | eventos de alerta (com `resolution`) |
| POST   | `/api/alerts/{id}/ack` · `/api/alerts/ack-all` | reconhece alerta(s)   |
| GET    | `/api/alerts/rules` · POST `/api/alerts/rules/{id}/toggle` | regras de alerta |
| PUT    | `/api/alerts/rules/{id}/channels` · `.../cooldown` | routing por-regra · cooldown |
| GET    | `/api/agents`                         | frota consolidada (online + offline) |
| POST   | `/api/agents/{id}/ping`               | ping ativo (round-trip, latência) |
| GET    | `/ws/web?token=...`                   | WS para web (events)           |
| GET    | `/ws/agent?token=...&id=...&caps=...` | WS para agent (WebSocket)      |
| GET    | `/api/agent/poll?id=...&caps=...`     | dispatch via long-poll (`-transport=http`) |
| POST   | `/api/agent/result` · `/api/agent/output` | resultado + stream do agente |

---

## 🤖 Agent

> Pasta [`agent/`](agent) · executor Go. Rode **um agente em cada máquina** que deve
> executar jobs (seu laptop, um server on-prem, uma VM/EC2, etc.).

Conecta ao `regente-server` via WebSocket **outbound** — sem abrir portas na máquina
do agente (atravessa NAT/firewall). Mesmo modelo de runner do GitHub Actions / GitLab /
Control-M Agent. Executa jobs localmente, devolve o resultado e **streama stdout/stderr
em tempo real** (aparece no detalhe da instance).

### Executores

| jobType | O que faz | Params (actionConfig) |
|---|---|---|
| `COMMAND` | Comando no shell do SO (`powershell -Command` no Windows, `sh -c` no Linux) | `command`, `cwd?` |
| `SCRIPT` | Executa um script; interpretador pela extensão (`.ps1`/`.bat`/`.sh`) | `scriptPath`, `args?`, `cwd?` |
| `HTTP` | Chamada REST com validação de status | `method`, `url`, `headers?`, `body?`, `expectStatus?` |
| `DATABASE` | SQL em Postgres/MySQL/SQLite (drivers pure-Go, sem client no host) | `driver`, `dsn`, `sql`, `maxRows?` |
| `FILE_WATCH` | Espera um arquivo chegar (e estabilizar) no host do agente | `path`, `intervalSec?`, `stableSec?` |
| `FILE_TRANSFER` | **MFT nativo**: transfere arquivos entre local, SFTP e S3 — glob na origem, escrita atômica, checksum SHA-256, move (alias `MFT`) | `src`, `dst`, `checksum?`, `deleteSource?`, `overwrite?`, … |
| `LAMBDA` | Invoca uma função AWS Lambda (SigV4 na stdlib, sem SDK) | `function`, `region?`, `payload?` |
| `GCP_RUN` | Dispara um Cloud Run Job (Run Admin API v2) | `project`, `region`, `job` |
| `WASM` | Roda um módulo WebAssembly WASI sandboxed via [wazero](https://wazero.dev) (pure-Go, sem CGO) | `wasmPath` \| `wasmUrl`, `args?`, `stdin?` |
| `K8S_JOB` | Cria um **Kubernetes Job** via API REST e aguarda concluir (adapter de nuvem por capability `K8S`) | `image`, `command?`, `namespace?`, `apiServer?`, `token?` |

> `SSH` (comando remoto agentless) **não** usa o agente — roda no próprio server.
> `K8S_JOB` exige um agente que anuncie `-caps K8S` (com acesso à API do cluster).

**Transporte** (`-transport`): `ws` (WebSocket, default), `http` (long-poll) ou
`sse` (Server-Sent Events, push imediato). Os dois últimos são
serverless-friendly — modelo outbound, control plane stateless/scale-to-zero.
Ver [`docs/arquitetura-futuro.md`](docs/arquitetura-futuro.md).

### Build & rodar (foreground)

```bash
cd agent
go build -o regente-agent .       # (.exe no Windows)
./regente-agent \
  -server ws://SEU-SERVER:8080/ws/agent \
  -token  rgta_...        # Settings → Agentes → Criar token \
  -id     meu-host \
  -caps   COMMAND,SCRIPT,HTTP
```

Token: gere um **token por agente** na UI (Settings → Agentes → Criar token). O
`dev-token` legado também funciona em dev. Sem token válido, o handshake é negado.

### Instalar como serviço

**Linux (systemd):**
```bash
sudo SERVER=ws://host:8080/ws/agent TOKEN=rgta_xxx ID=$(hostname) \
     CAPS=COMMAND,SCRIPT,HTTP USER=$USER ./deploy/install-linux.sh
journalctl -u regente-agent -f     # logs
```

**Windows (Tarefa Agendada — roda no boot, reinicia sozinho), PowerShell como Administrador:**
```powershell
.\deploy\install-windows.ps1 -Server ws://host:8080/ws/agent -Token rgta_xxx `
                             -Id $env:COMPUTERNAME -Caps COMMAND,SCRIPT,HTTP
Get-ScheduledTask RegenteAgent     # status
```

### Dispatch: como o server escolhe o agente

- Job com **agente específico** (campo "Agente (onde roda)" no JobConfigDrawer) →
  vai direto pra ele.
- Senão → o server escolhe um agente online cuja **capability** bate com o jobType
  (`PickAgent`). Por isso `-caps` deve incluir os jobTypes que o agente aceita.
- **Ambiente** (`environment` no job × flag `-env` do agente): lado sem label = coringa;
  os dois com label = precisam bater (case-insensitive). Job `prod` **nunca** cai num agente
  `dev` — nem pinado (fica em WAIT AGENT com o motivo no Explain).

### Protocolo WebSocket

```jsonc
// server → agent
{ "event":"dispatch", "instanceId":"...", "jobType":"COMMAND", "params":{...}, "timeout":300 }
// agent → server (streaming durante a execução)
{ "event":"output", "instanceId":"...", "chunk":"linha de stdout/stderr" }
// agent → server (final)
{ "event":"result", "instanceId":"...", "exitCode":0, "output":"saída completa" }
// agent → server (a cada 30s)
{ "event":"heartbeat" }
```

---

## 🎨 Frontend

> Pasta [`app/`](app) · React + TypeScript + Vite. Cliente HTTP/WebSocket do `regente-server`.

| Camada | Tecnologia |
|---|---|
| Frontend (este repo) | React + TypeScript + Vite, [@xyflow/react](https://reactflow.dev) (canvas), ícones lucide |
| Backend | Go (`regente-server`) — GitOps + SQLite/Postgres, WebSocket hub |
| Executor | Go (`regente-agent`) — conexão outbound (WS · HTTP long-poll · SSE), roda COMMAND/SCRIPT/HTTP/SSH/WASM |
| Fonte da verdade | Repositório GitHub (YAML em `definitions/<folder>/<id>.yaml`) |

### Rodando o frontend

Pré-requisitos: Node 18+ e o `regente-server` rodando.

```bash
cd app
npm install
cp .env.example .env        # configure VITE_REGENTE_SERVER_URL
npm run dev                 # http://localhost:5173
```

Variáveis (`.env`):

```bash
VITE_REGENTE_SERVER_URL=http://localhost:8080   # vazio = modo local (localStorage)
VITE_REGENTE_TOKEN=dev-token
```

`VITE_REGENTE_SERVER_URL=@origin` = **same-origin**: o server Go serve o SPA na mesma
porta (`-spa-dir`), então a UI usa `window.location.origin` em runtime — é o modo usado
pra hospedar a demo atrás de um túnel (a URL pode mudar sem rebuildar o front).

Login padrão de dev: `admin` / `admin`.

### Hospedar pra outras pessoas testarem (demo com link público)

Pra convidar amigos a testar a UI (criar e executar jobs) num link https, o server serve
o SPA single-origin (`-spa-dir`) e um agente roda isolado em Docker. Passo-a-passo,
script (`host-demo.ps1`) e notas de segurança em [`deploy/demo/README.md`](deploy/demo/README.md).

### Site de docs (docs-as-code)

Os markdown deste repo (READMEs + `docs/*.md`) viram um site estático self-contained
(zero CDN, CSS inline) com `go run ./cmd/docsite -repo .. -out ../docs/site` (do diretório
`server/`); o server serve o resultado em `/docs` com a flag `-docs-dir` (single-origin,
mesmo padrão do `-spa-dir`). Não existe conteúdo próprio do site — editar o markdown e
regenerar é o fluxo inteiro.

### Estrutura (frontend)

```
src/
├── v2/                  # UI atual (Monitoring, Design, drawers, dialogs)
│   ├── V2Preview.tsx    # shell principal (topbar, canvas, modos)
│   ├── JobConfigDrawer  # edição de job (Geral/Schedule/Calendars/Action/Deps)
│   ├── ScheduleEditor   # scheduler visual estilo Control-M
│   ├── AlertsPanel.tsx  # tela de alertas (eventos + regras) — Fase 8
│   └── ...
├── lib/                 # clientes de API + modelo + adapters
│   ├── server-client.ts # REST + WS
│   ├── git-api.ts       # status, token, cleanup, deep-links
│   ├── agents-api.ts    # agentes online
│   ├── alerts-api.ts    # alertas (facade dual-mode server/local)
│   └── adapters/        # ports & adapters (storage/scheduler/executor)
└── main.tsx
```

---

## 🛠 Desenvolvimento

Três terminais — server, agent e frontend:

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

## 🗺 Roadmap

Todo o planejamento vive em **[`docs/roadmap.md`](docs/roadmap.md)** — a **fonte única de status**:

- **✅ Entregue** — tudo que já foi construído e validado, **detalhado por tópico** (pra depois virar doc/feature).
- **🔜 Backlog** — o que falta, com IDs estáveis e specs (a lista que a gente vai fazendo crescer).
- **📜 Changelog** — o mesmo em ordem cronológica, por data/commit, com o "porquê" de cada entrega.

Este README é só a **apresentação do produto** (conceito, capacidades, arquitetura, guias). Se algo aqui
divergir do roadmap, o roadmap vence.

Para a **história do projeto** — o problema, as apostas de arquitetura, a semântica Control-M nas
bordas, a escala validada a 1M jobs/dia e as lições — ver o **[case study](docs/case-study.md)**.

---

<div align="center">
<sub>Projeto pessoal de portfólio. UX inspirada na operação do Control-M; nenhuma relação com a BMC.</sub>
</div>
