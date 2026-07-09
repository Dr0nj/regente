# 🤖 Regente MCP — camada agent-native

`regente-mcp` é um servidor [MCP (Model Context Protocol)](https://modelcontextprotocol.io)
que expõe os **diferenciais determinísticos** do Regente como _tools_ pra um agente
(Claude Desktop, etc.). A ideia: você **opera o Regente conversando** — *"o que falhou
em pagamentos hoje e por quê?"* — e o agente chama as tools, que devolvem a verdade
que o engine computou (o LLM **narra**, não inventa).

> Arquitetura: é uma **fachada** sobre a API REST — não toca o core do servidor.
> Pure-Go stdlib, JSON-RPC 2.0 sobre stdio (o transporte que o Claude Desktop usa).

## Tools

Read-only (sempre disponíveis) — **11**:

| Tool | O que faz | Endpoint |
|---|---|---|
| `daily_summary` | panorama do dia (total · por status · por folder) | `/api/instances/summary` |
| `forecast` | previsão de agendamento (dry-run); `days>1` prevê a janela ≥1 semana | `/api/forecast` · `/api/forecast/range` |
| `list_instances` | acha instances por folder/status/busca (pra pegar o `instanceId`) | `/api/instances` |
| `explain_job` | por que um job (não) rodou — gating estruturado | `/api/instances/{id}/explain` |
| `blast_radius` | impacto de cancelar/segurar um job (downstream · SLA) | `/api/instances/{id}/blast-radius` |
| `job_neighborhood` | grafo local (ancestrais/descendentes até `radius` saltos) | `/api/instances/{id}/neighborhood` |
| `root_cause` | causa raiz de uma falha/bloqueio (sobe a cadeia de upstreams falhos) | `/api/instances/{id}/rca` |
| `diff_daily` | o que mudou entre duas diárias | `/api/daily/diff` |
| `dry_run` | simula uma daily futura (roda/espera/nunca) | `/api/daily/dryrun` |
| `event_log` | feed de eventos do dia (cross-instance), filtrável | `/api/events` |
| `query` | pergunta em linguagem natural (PT/EN) → resposta determinística | `/api/query` |

Escrita (só com `-allow-writes`; o cliente MCP ainda pede aprovação por chamada) — **11**, todas **destructiveHint**:

| Tool | O que faz | Endpoint |
|---|---|---|
| `hold_job` | segura um job WAITING → HELD (Control-M Hold) | `POST /api/instances/{id}/hold` |
| `release_job` | libera um job HELD → WAITING (Release) | `POST /api/instances/{id}/release` |
| `cancel_job` | cancela um job do dia (→ CANCELLED) | `POST /api/instances/{id}/cancel` |
| `confirm_job` | confirma um job no gate WAIT_CONFIRM (Control-M Confirm) | `POST /api/instances/{id}/confirm` |
| `rerun_job` | re-executa um job (→ WAITING) | `POST /api/instances/{id}/rerun` |
| `set_ok` | marca NOTOK/CANCELLED como OK, destrava sucessores | `POST /api/instances/{id}/set-ok` |
| `force_order` | ordena e roda uma **definition** agora, fora do schedule | `POST /api/definitions/{id}/force` |
| `pause_folder` | pausa um workflow inteiro (WAITING→HELD em massa, estado preservado) | `POST /api/folders/{name}/pause` |
| `resume_folder` | retoma o workflow (HELD→WAITING em massa) | `POST /api/folders/{name}/resume` |
| `bulk_action` | uma ação em N instâncias (transacional por item, máx 500) | `POST /api/bulk/instances` |
| `ingest_event` | seta conditions e/ou força um job via evento externo (idempotente) | `POST /api/events/ingest` |

## Build & run

```bash
cd server && go build -o regente-mcp ./cmd/mcp
REGENTE_URL=http://localhost:8080 REGENTE_TOKEN=<token> ./regente-mcp
# escrita (hold/release/cancel/confirm/rerun/set-ok · force_order ·
# pause/resume_folder · bulk_action · ingest_event), opcional:
./regente-mcp -allow-writes
```

## Claude Desktop

Em `claude_desktop_config.json` (Settings → Developer → Edit Config):

```json
{
  "mcpServers": {
    "regente": {
      "command": "/caminho/para/regente-mcp",
      "env": {
        "REGENTE_URL": "http://localhost:8080",
        "REGENTE_TOKEN": "seu-token"
      }
    }
  }
}
```

Reinicie o Claude Desktop. Aí é só perguntar: *"usando o regente, me dá o resumo da
daily de hoje"*, *"por que o job fechamento-2026-06-24 não rodou?"*, *"se eu cancelar
o PIX_ENVIO agora, qual o impacto?"*, *"o que mudou na daily de hoje vs ontem?"*,
*"prevê a próxima semana"*. Com `-allow-writes` o agente também **age** (sempre com
aprovação): *"segura a folder PIX até eu liberar"*, *"marca o fechamento como OK e
destrava os sucessores"*, *"força o etl-vendas agora"*, *"o arquivo chegou: seta a
condition ARQ_VENDAS_OK"*.

## Segurança / postura

- **Read-only por padrão.** Writes exigem `-allow-writes` no servidor **E** aprovação
  humana do cliente MCP em cada chamada (dupla trava).
- **Determinístico.** As tools devolvem o que o scheduler já computa (gating, grafo de
  deps, snapshots congelados). O LLM narra — nunca decide scheduling.
- O token é o mesmo Bearer da API; dê a ele só o papel (RBAC) que o uso exige.
