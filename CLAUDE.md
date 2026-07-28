# CLAUDE.md — memória do projeto Regente

Orientação para agentes trabalhando neste repositório. Detalhe de produto está
no [`README.md`](README.md); decisões de arquitetura futura em
[`docs/architecture-future.md`](docs/architecture-future.md).

## Fontes da verdade (ler ANTES de agir)

| Pergunta | Onde responde |
|---|---|
| "o que falta / o que já foi entregue?" | [`docs/roadmap.md`](docs/roadmap.md) — **fonte única de status**. Ao entregar: tira do §🔜 Backlog, escreve no §✅ Entregue, soma linha no §📜 Changelog |
| "como dependências/condições/rerun/Set OK/Force se comportam?" | [`docs/conditions-events.md`](docs/conditions-events.md) — **spec obrigatória**, ler o arquivo inteiro antes de tocar nessa área; mudou semântica, muda spec + testes no MESMO commit |
| "o que o produto faz / como instala?" | [`README.md`](README.md) (apresentação; não repete roadmap) |

## O que é

Orquestrador de jobs estilo enterprise clássico, **Git-native**. Monorepo:

| Pasta | Stack | Papel |
|---|---|---|
| `server/` | Go (chi + WebSocket + SQLite/Postgres) | control plane: scheduler, API, hub, GitOps |
| `agent/` | Go | executor local, conexão outbound; 13 jobTypes (COMMAND · SCRIPT · HTTP · DATABASE · FILE_WATCH · FILE_TRANSFER · WASM · K8S · LAMBDA · BATCH · GLUE · STEP_FUNCTION · GCP_RUN). Catálogo de schema por tipo em `server/internal/domain/typeschema.go` |
| `app/` | React + TS + Vite | UI (Monitoring + Design); dual-mode server/localStorage |

Fonte da verdade das definitions = YAML num repo GitHub **separado**, que é do
operador e **diferente em cada instalação** — não existe default de fábrica
(`-git-source` vazio = modo offline, só disco). Nunca reintroduzir o repo do lab
como default: ele dá 404 pra qualquer outra pessoa.

## Comandos

```bash
# server
cd server && go build ./... && go vet ./... && go test ./...
# agent
cd agent  && go build ./... && go vet ./... && go test ./...
# app
cd app && npm ci && npm run build      # tsc -b && vite build
cd app && npm run lint                 # eslint — GATE da CI, tem que ficar em ZERO
# staticcheck (GATE da CI nos dois módulos Go)
cd server && go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...
# doc: o site é build VERSIONADO — regenere depois de mexer em qualquer markdown
cd server && go run ./cmd/docsite -repo .. -out ../docs/site
```

**3 workflows**, todos disparando na `main`:

- **`ci.yml`** — server (build · vet · **staticcheck** · deadcode informativo ·
  test · **docs/site atualizado**), agent (build · staticcheck · test), app
  (`npm ci` · **lint** · build). Também roda em PR.
- **`pages.yml`** — publica `docs/site/` no GitHub Pages (push que toca doc).
- **`release.yml`** — **TODO push na `main` publica release**, com
  `scripts/smoke-install.sh` (instala num systemd real em container) como portão.
  Pra pular: `[no release]` no **assunto** do commit.

## Convenções

- **Idioma (fronteira dura):** **inglês = tudo que o USUÁRIO lê** — UI, mensagens
  do server, CLI, output do agente, contrato OpenAPI, scripts de instalação,
  READMEs e `docs/*.md` publicados. **pt-BR = tudo que o DEV lê** — comentário de
  código, mensagem de commit, `t.Fatalf`, `docs/roadmap.md` e planos internos.
  Não traduzir termos canônicos do domínio (Schedule · On/Do · ODAT · Force Order ·
  Set OK…), identificadores de máquina, nem os sinônimos PT do parser de NL-query
  (são ENTRADA do usuário).
- **Go:** sempre `gofmt -w` os arquivos TOCADOS antes de commitar (nunca a árvore
  inteira). Idioma dos comentários: português. Sem CGO (SQLite via modernc; WASM
  via wazero).
- **Commits:** Conventional Commits em português (`feat:`, `fix:`, `docs:`…).
- **Push:** o dono autorizou **commitar e dar push direto na `main`** (sem
  branch/PR), salvo se houver branch protection — aí cai em branch + PR. Puxe o
  remoto antes: o dono edita o roadmap direto no GitHub.
- **Modelo/identidade:** nunca colocar o id do modelo em commits/PRs/código.

## Tooling de agente (Claude Code)

- **`go.work`** (raiz) — workspace amarrando `server` + `agent`. Da raiz dá pra rodar
  `go test ./server/... ./agent/...` ou targeted `go test ./server/internal/<pkg>/...` **sem
  `cd`/`go -C`**. ⚠ `go build ./...` PURO não funciona da raiz (a raiz não é módulo) — use
  `./server/...` / `./agent/...`. Os comandos por-módulo da CI seguem funcionando.
- **`scripts/verify.sh`** — equivalente local da CI (server build+vet+test · agent build+test ·
  app build). `bash scripts/verify.sh`. Slash: **`/verify`**.
- **`/new-migration <desc>`** — scaffold de `schemaVN` lembrando de registrar nas DUAS slices
  (`sqliteMigrations` E `pgMigrations`).
- **`.claude/settings.suggested.json`** — TEMPLATE inerte (o Claude Code não carrega). Para ATIVAR
  pré-aprovação de comandos seguros + hook que roda `gofmt -w` em todo `.go` editado:
  `cp .claude/settings.suggested.json .claude/settings.json`. Overrides pessoais (não versionados):
  `.claude/settings.local.json`. Hook em `.claude/hooks/format_go.py` (defensivo: no-op silencioso se falhar).

## Arquitetura serverless (aplicada — ver ADR)

"Serverless portátil" (container scale-to-zero + estado/gatilho externalizados),
**não FaaS**, **sem lock-in AWS**. Tudo opt-in; defaults = daemon clássico.

- **Fase 1 — gatilho externo:** `Scheduler.Tick()` idempotente; flags
  `-scheduler=internal|external` e `-role=all|api|scheduler`; `POST /api/scheduler/tick`.
  Artefatos em [`deploy/`](deploy) (Dockerfile distroless, Knative, CronJob).
- **Fase 2 — transporte plugável:** interface `scheduler.Bus` + transporte HTTP
  long-poll (`agent -transport=http`; `agentBroker` + `/api/agent/poll|result|output`).
- **Fase 3 — executores como plugins:** roteamento por capability é o seam;
  executor **WASM** (`jobType: WASM`, wazero), **adapters de nuvem** (AWS
  Lambda/Batch/Glue/Step Functions · GCP Cloud Run Jobs · K8S Job), **NATS**
  (`-bus=nats`) e **OTel** entregues. Durable execution (Temporal/Restate) e
  Postgres-como-fila foram **decididos NÃO fazer** (ver tabela §7 do ADR) —
  não são pendência.

## Features entregues (histórico)

- **Alerting (Fase 8):** motor de regras (`server/internal/scheduler/alerting.go`
  + `app/src/lib/alerting.ts`), tela `app/src/v2/AlertsPanel.tsx` (sino + badge),
  API `/api/alerts*`, broadcast `alert.fired`. Dual-mode (Postgres/localStorage).
  **Routing externo multi-canal:** sinks Slack (`alert_slack_webhook`), webhook
  genérico (`alert_webhook_url`), e-mail SMTP (`alert_smtp_*`, `net/smtp`) e
  PagerDuty Events v2 (`alert_pagerduty_routing_key`) — credenciais mascaradas em
  `/api/settings`. **Routing por-regra:** cada regra escolhe os canais em `channels`
  (`PUT /api/alerts/rules/{id}/channels`); `channelWanted` decide o disparo e
  regra sem canal externo cai em fallback "todos os sinks configurados" (back-compat).
  Router best-effort async em `alerting.go`; UI na aba Regras (chips por regra +
  config de sinks, admin). **Cooldown por (regra×job)** (`cooldownKey`): re-disparos
  do MESMO job são agrupados (anti-spam de flapping), mas jobs DIFERENTES numa rajada
  nunca se suprimem — garante que nenhum erro distinto seja perdido na tela de alertas.
  **Ciclo de vida do alerta** (`alert_events.resolution`, migration v3): ''=novo ·
  'ack' · 'rerun' · 'set_ok'. Rerun/SetOK de uma instância marcam os alertas pendentes
  do job (`MarkHandledByWorkflow`, hook nos handlers + broadcast `alert.changed`);
  alertas já tratados não são sobrescritos. `rule-failure` vem com cooldown **0** (toda
  falha vira alerta; dedup é por tratamento). Cooldown editável: `PUT .../cooldown`.
- **Temas** (`app/src/lib/theme.ts` + `data-theme` em `tokens.css`): **17 temas**
  (13 escuros + 4 claros), Escuro é o default. Seção "Tema" no `SettingsDialog` com
  cards de swatch; persiste em localStorage; aplicado no boot (`main.tsx`).
  **GOTCHA-RAIZ: são DOIS sistemas de token** — `--v2-*` E os `--color-*` do Tailwind
  que o body e o canvas do ReactFlow leem; tema claro tem de sobrescrever os DOIS,
  senão fundo/grafo ficam pretos. **Toda a UI
  consome os tokens `--v2-*`** — cor chumbada num componente = ele não acompanha o tema
  (foi o bug do `ControlMPanel`, corrigido: trocado tudo por tokens). **Borda neon:** token
  `--v2-accent-glow` por tema + classe `.v2-neon-card` (borda 1px na cor do tema + halo)
  nos diálogos de config/senha/tema — `SettingsDialog`, `UsersDialog` (lista/reset senha/ACLs),
  `UserMenu` (menu + trocar senha), `AlertsPanel` e `ControlMPanel`. A classe só adiciona
  borda+sombra: **se o card tiver `border`/`boxShadow` inline, remova** (inline ganha da
  classe e mata o neon — caso do `AlertsPanel`).
- **Serverless Fases 1–3** (acima).

## Gotchas

- **npm optional deps (bug #4828):** `app/package-lock.json` precisa conter as
  entradas instaláveis dos binários nativos (`@rolldown/binding-linux-x64-gnu`,
  `lightningcss-linux-x64-gnu`, `@tailwindcss/oxide-linux-x64-gnu`). Se o `vite
  build` quebrar com "Cannot find native binding", regenere o lockfile:
  `rm -rf node_modules package-lock.json && npm install`.
- **Fixture WASM:** `agent/testdata/echo.wasm` é compilado de
  `testdata/echo/main.go` com `GOOS=wasip1 GOARCH=wasm go build`. É grande (~2.4MB,
  saída padrão do Go) mas committado para o teste ser hermético.
- **eslint está em ZERO e é catraca (RH-4):** `set-state-in-effect`, `refs`,
  `immutability` e `only-export-components` são **`error`** em
  `app/eslint.config.js` — código novo não passa no gate se regredir. Não existe
  mais "lint pré-existente" pra ignorar (o antigo aviso sobre `V2Preview.tsx`
  morreu com a trilha RH). Precisa violar? **anote** com
  `// eslint-disable-next-line <regra> -- <motivo>; ver roadmap §RH` — nunca
  rebaixe a regra. GOTCHA: `set-state-in-effect` flagra a CHAMADA de qualquer
  função que seta state no corpo do efeito; a saída é escopo async aninhado
  (`useEffect(() => { void (async () => { … })(); }, [])`).
- **Scheduler stateful:** o tick interno (modo `internal`) reload de defs no boot
  + on-save; no modo `external` as defs são carregadas no boot (`ReloadDefs`).
