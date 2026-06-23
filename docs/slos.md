# 🎯 SLOs do control plane — regente-server

> Objetivos de nível de serviço do "cérebro" do Regente, cada um amarrado a uma **métrica**
> (`/metrics`), a um **probe** (`/livez` · `/readyz`) e a um **alerta automático** (R7,
> `-selfmon`). É assim que o orquestrador vigia a si mesmo — ver [`roadmap.md`](roadmap.md)
> (trilha Resiliência R1–R7) e a validação de chaos/HA já feita.

| # | SLO | Alvo | Sinal (métrica/probe) | Alerta (R7) |
|---|-----|------|------------------------|-------------|
| 1 | **Disponibilidade da API** | ≥ 99.9% de `GET /readyz` = 200 | `/readyz` (gate = DB alcançável) | `db-unreachable` |
| 2 | **Tempo de failover** (líder cai → novo líder) | ≤ 10 s | `regente_is_leader` (changes) | `leader-flapping` (se trocar demais) |
| 3 | **Frescor do scheduling** (loop vivo) | tick < 90 s | `regente_scheduler_last_tick_age_seconds` | `tick-stalled` |
| 4 | **Capacidade de execução** (frota de agentes) | sem quedas não-planejadas | `regente_agents_online` | `agents-drop` |
| 5 | **Liveness do processo** | reinício automático se travar | `/livez` + `livenessProbe` | (supervisor R1) |
| 6 | **RPO/RTO (DR)** | RPO ≤ intervalo de backup · RTO ≤ minutos | backup R6 (`-backup`/`pg_dump`) | runbook [`dr-backup.md`](dr-backup.md) |

## Como cada SLO é observado e defendido

- **1. Disponibilidade** — `/readyz` só responde 200 se o DB está alcançável (gate duro). O
  `readinessProbe` do k8s tira o pod de rotação quando viola; o R7 alerta `db-unreachable`.
- **2. Failover** — só o líder roda daily/dispatch; ao morrer, o advisory lock é liberado e
  outro nó assume. **Medido ~4 s** na validação 2-nós real. `regente_is_leader` permite
  alertar `changes()` no Prometheus (flapping) além do alerta nativo do R7.
- **3. Frescor** — todo nó marca `lastTick` a cada ciclo; idade exposta no gauge e no
  `/readyz`. Acima de 90 s o R7 dispara `tick-stalled` (jobs podem não estar promovendo).
- **4. Capacidade** — queda de `regente_agents_online` versus o baseline gera `agents-drop`.
- **5. Liveness** — `/livez` responde enquanto o processo serve; o supervisor (systemd
  `Restart=always` / Windows Service / `livenessProbe`) reinicia um processo travado.
- **6. DR** — backup online (`-backup` = VACUUM INTO no SQLite; `pg_dump`/PITR no Postgres);
  restore validado pelo drill do runbook (`/readyz` 200 + `/api/env` pós-restore).

## Verificação contínua

- **Unit/integração** — `go test ./...` (inclui `/readyz`, mTLS handshake, RBAC, selfmon eval,
  backup round-trip, **E2E HTTP** e **smoke de carga** ~7.5k req/s local).
- **Chaos/HA** — `server/deploy/chaos-ha.sh` automatiza: 2 nós no mesmo Postgres → 1 líder →
  mata o líder → confere failover. (resultado já registrado no roadmap, 2026-06-23.)
- **Carga real** — para volume de produção (10k+ jobs/dia, p99), rodar `hey`/`k6` contra um
  deploy real fica como item de Qualidade aberto no roadmap.
