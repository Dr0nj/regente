# ⚙️ Operação contínua — Enterprise readiness

> Como operar o `regente-server` em produção sem perder disponibilidade nem correção:
> **upgrades zero-downtime**, **multi-ambiente** (Dev/Staging/Prod), **quotas** que
> sobrevivem a failover e **reconciliação de drift** GitOps. Liga-se à trilha
> Resiliência (R1–R7, ver [`roadmap.md`](roadmap.md)) e aos SLOs ([`slos.md`](slos.md)).

## 1. Upgrades zero-downtime (rolling)

O control plane é **stateless** (todo estado durável vive fora do processo: Postgres +
workspace Git) e usa **leader election por advisory lock** (G1). Isso torna o rolling
upgrade um caso particular do failover já validado em chaos/HA:

```
1. Nós A (vN) e B (vN) atrás de um LB/VIP, no MESMO Postgres → A é líder, B follower.
2. Sobe o nó C com a versão NOVA (vN+1) → entra como follower (advisory lock ocupado).
3. Espera C ficar READY (/readyz = 200) — só então recebe tráfego do LB.
4. Drena e derruba um nó ANTIGO. Se era o líder, o lock é liberado e outro nó
   (incl. C) assume em ~4s (medido no chaos/HA).
5. Repete até toda a frota estar em vN+1.
```

**Por que é zero-downtime para a API:** um follower serve a API normalmente (`/readyz`
só gate em DB, não em liderança — R3). Durante a janela de ~4s de failover **a API
continua respondendo** pelos followers; só a materialização da daily/dispatch pausa
brevemente — e é **idempotente** (claim atômico + checagem de existência), então retomar
não duplica nem perde nada.

**Migrations:** as migrations são **idempotentes e versionadas** (`schema_migrations`).
Um nó vN+1 aplica o que falta no boot; vN e vN+1 coexistem desde que a migration seja
aditiva (regra: nunca remover/renomear coluna na mesma release que o código que a usa —
expand/contract em duas releases).

Demonstração automatizada: [`../server/deploy/rolling-upgrade.sh`](../server/deploy/rolling-upgrade.sh)
(sobe 2 nós no mesmo PG, promove o novo, drena o antigo e mede o gap de liderança).

## 2. Multi-ambiente (Dev / Staging / Prod)

Cada ambiente é um **deployment independente** — nada de flags mágicas dentro de um único
processo. O isolamento é por **branch do workspace + state store + label**:

| Eixo | Dev | Staging | Prod |
|------|-----|---------|------|
| Workspace (defs) | branch `dev` | branch `staging` | branch `main` |
| State store | PG/SQLite dev | PG staging | PG prod (HA) |
| `env_label` | `DEV` | `STAGING` | `PROD` |

```sh
# exemplo Prod
regente-server -db-driver postgres -db "$PROD_DSN" \
  -git-branch main -github-repo Dr0nj/regente-workspace
# env_label setado via UI (Settings → Ambiente) ou seed; aparece em /api/env e /metrics
```

**Promoção** = Git flow: um PR `dev → staging → main` no `regente-workspace` promove as
definitions de um ambiente ao próximo (revisável, auditável, reversível por `git revert`).

**Observabilidade por ambiente:** o `/metrics` expõe `regente_env_info{env="PROD"} 1`;
dashboards e alertas agrupam/filtram por esse label, então um Prometheus único cobre os
três ambientes sem confundir séries.

## 3. Quotas (recursos) através de failover

As quotas (F15 — *quantitative resources*, no estilo Control-M) limitam quantos jobs
concorrem por um recurso nomeado (ex.: `db=5` → no máx. 5 jobs usando o pool ao mesmo
tempo). O tracker é **in-memory e vive no líder**. Ao assumir liderança (boot ou
failover), o novo líder **reconstrói o uso a partir das instances `RUNNING`** no estado
durável (cada instance carrega o snapshot da def, com os recursos) —
`RebuildResourcesFromRunning`. Sem isso, um líder recém-promovido começaria com o tracker
zerado e deixaria estourar a capacidade. Coberto por teste (`TestQuotas_RebuildFromRunning`).

## 4. Reconciliação de drift (GitOps)

O repositório é o estado **desejado**; o runtime carrega uma cópia local. Quando o remoto
avança (push, merge de PR), o runtime fica em **drift**.

- **Auto-sync** (já existente): `-git-poll-interval` faz `fetch+reset+reload` periódico —
  o runtime converge ao Git sozinho. Drift também vira evento `git.drift` na UI.
- **Reconciler operacional** (`-drift-reconcile-sec`, opt-in): só o **líder** roda; quando
  detecta drift, **alerta pelos mesmos canais do R7** (Slack/webhook/e-mail/PagerDuty) —
  não só um badge na UI. Dois modos:
  - `-drift-reconcile-mode=alert` (default): **não** mexe no workspace, só **avisa** quem
    está de plantão. Ideal para ambientes regulados / `pr-required`, onde reset automático
    não é permitido.
  - `-drift-reconcile-mode=sync`: **reconcilia sozinho** (fetch+reset+reload). Útil quando o
    git-poll está desligado e você quer convergência periódica + alerta se a convergência
    falhar.

```sh
# Prod regulado: avisa, não reseta sozinho
regente-server ... -git-poll-interval 0 -drift-reconcile-sec 60 -drift-reconcile-mode alert
```

## 5. Checklist de produção

```
[ ] State store FORA do container efêmero (PG gerenciado, ou volume p/ SQLite) — R4/R6
[ ] Supervisor com restart automático (systemd Restart=always / Windows Service / k8s) — R1
[ ] /livez no livenessProbe · /readyz no readinessProbe — R2/R3
[ ] -selfmon ON com canais de alerta configurados (Slack/PagerDuty) — R7
[ ] Backup agendado (backup.sh / pg_dump) + drill de restore — R6
[ ] env_label setado por ambiente; Prometheus agrupando por regente_env_info
[ ] -drift-reconcile-sec ligado (alert em regulado, sync caso contrário)
[ ] Rolling upgrade testado (rolling-upgrade.sh) antes da 1ª subida real
```
