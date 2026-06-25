# 🤖 Regente MCP — camada agent-native

`regente-mcp` é um servidor [MCP (Model Context Protocol)](https://modelcontextprotocol.io)
que expõe os **diferenciais determinísticos** do Regente como _tools_ pra um agente
(Claude Desktop, etc.). A ideia: você **opera o Regente conversando** — *"o que falhou
em pagamentos hoje e por quê?"* — e o agente chama as tools, que devolvem a verdade
que o engine computou (o LLM **narra**, não inventa).

> Arquitetura: é uma **fachada** sobre a API REST — não toca o core do servidor.
> Pure-Go stdlib, JSON-RPC 2.0 sobre stdio (o transporte que o Claude Desktop usa).

## Tools

Read-only (sempre disponíveis):

| Tool | O que faz | Endpoint |
|---|---|---|
| `daily_summary` | panorama do dia (total · por status · por folder) | `/api/instances/summary` |
| `list_instances` | acha instances por folder/status/busca (pra pegar o `instanceId`) | `/api/instances` |
| `explain_job` | por que um job (não) rodou — gating estruturado | `/api/instances/{id}/explain` |
| `blast_radius` | impacto de cancelar/segurar um job (downstream · SLA) | `/api/instances/{id}/blast-radius` |
| `diff_daily` | o que mudou entre duas diárias | `/api/daily/diff` |
| `dry_run` | simula uma daily futura (roda/espera/nunca) | `/api/daily/dryrun` |

Escrita (só com `-allow-writes`; o cliente MCP ainda pede aprovação por chamada):

| Tool | O que faz |
|---|---|
| `rerun_job` | re-executa um job (→ WAITING) — **destructiveHint** |
| `set_ok` | marca NOTOK/CANCELLED como OK — **destructiveHint** |

## Build & run

```bash
cd server && go build -o regente-mcp ./cmd/mcp
REGENTE_URL=http://localhost:8080 REGENTE_TOKEN=<token> ./regente-mcp
# escrita (rerun/set-ok), opcional:
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
o PIX_ENVIO agora, qual o impacto?"*, *"o que mudou na daily de hoje vs ontem?"*.

## Segurança / postura

- **Read-only por padrão.** Writes exigem `-allow-writes` no servidor **E** aprovação
  humana do cliente MCP em cada chamada (dupla trava).
- **Determinístico.** As tools devolvem o que o scheduler já computa (gating, grafo de
  deps, snapshots congelados). O LLM narra — nunca decide scheduling.
- O token é o mesmo Bearer da API; dê a ele só o papel (RBAC) que o uso exige.
