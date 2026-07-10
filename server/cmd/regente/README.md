# `regente` — CLI de Developer Experience

Onde o Control-M perde feio: o ciclo **definir → testar → rodar local → promover → operar**
vira linha de comando + Git, sem console proprietário no meio. Fecha os diferenciais D-6..D-9
e o ADV-6 (`ops` + SDK Go).

```
go build -o regente ./cmd/regente
```

## `regente test <job.yaml | workspace-dir>` — D-7

Valida e **simula** sem servidor. Pipeline:

1. parse **estrito** (campo desconhecido = erro — pega typo antes do runtime);
2. validação estrutural (id/label/team, actionConfig por jobType — a mesma do save da API);
3. grafo: upstream para job inexistente · **ciclo** de dependências;
4. **policy as code** (D-10): `policies.yaml` do workspace, se houver;
5. **simulação da daily** de `-date` com o MESMO engine do servidor (`DryRun`/`IsScheduledOn`):
   quem RODA, quem ESPERA, quem NUNCA dispara.

Exit `0` = passou (warnings ok) · `1` = falhou → use direto no CI do repo de workspace.

```
regente test ./regente-workspace -date 2026-07-08        # workspace inteiro
regente test job.yaml -json                              # saída JSON pra CI
```

## `regente dev [daily]` — D-8

Um Regente inteiro, **descartável**, numa porta local: SQLite temp (estado morre com o
processo), demo-mode (sem agente, jobs mock-finalizam OK), workspace local (sem Git/push/rede),
daily materializada já no boot + ticker interno.

```
regente dev daily -workspace ./regente-workspace -date 2026-07-08 -addr :8686
```

## `regente promote -from <branch> -to <branch>` — D-9

Promoção multi-ambiente **Git-nativa**: ambientes são branches do repo de workspace. Promover =
o snapshot dos paths promovíveis da origem (definitions/, calendars/, **policies.yaml** — código E
política juntos) **substitui** o destino (add/update/**delete**, não merge). Commit revisável no
branch destino; o server daquele ambiente pega pelo fluxo GitOps normal.

```
regente promote -repo https://github.com/org/regente-workspace.git -from dev -to staging
regente promote -from dev -to main -folders financeiro,pix        # promoção parcial
regente promote -from dev -to main -dry-run                       # só o diff
```

> Flags podem vir em qualquer ordem em relação ao argumento posicional
> (`regente test ws -json` == `regente test -json ws`).

## `regente ops <subcomando>` — ADV-6

Opera um **server vivo**, construído 100% sobre o SDK Go (`pkg/client`) — o CLI não fala
HTTP direto; qualquer integração pode importar o mesmo pacote.

```
regente ops instances [-date D] [-status NOTOK,WAITING] [-folder F] [-late] [-group status] [-json]
regente ops action <hold|release|cancel|rerun|set-ok|confirm> <instanceId>
regente ops force <definitionId>
regente ops ingest -source ci -id build-123 -condition dados-ok
regente ops daily [-report] [-date D] [-json]
regente ops archives [list | get <arquivo> -o saida.ndjson]
regente ops jobtypes [-json]
```

Conexão: `-server`/`-token` ou envs `REGENTE_SERVER`/`REGENTE_TOKEN`.
A superfície é a **curada de integração** (query composta D-5, lifecycle, ingest D-3,
daily E5, archives ADV-5, catálogo ADV-1) — mesma lista do API-1 no roadmap.

### SDK Go (`pkg/client`)

```go
import "github.com/Dr0nj/regente-server/pkg/client"

cli := client.New("http://localhost:8080", os.Getenv("REGENTE_TOKEN"))
res, _ := cli.QueryInstances(client.Query{Statuses: []string{"NOTOK"}})
_ = cli.Action(res.Items[0].ID, "rerun")
```
