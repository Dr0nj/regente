# `regente` — CLI de Developer Experience

Onde o Control-M perde feio: o ciclo **definir → testar → rodar local → promover** vira
linha de comando + Git, sem console proprietário no meio. Fecha os diferenciais D-6..D-9.

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
